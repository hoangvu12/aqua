package picker

import (
	"context"
	"encoding/json"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"aqua/internal/config"
)

// reconcileInterval is the steady-state safety-net poll cadence. Responsiveness
// comes from the local event stream (riot.EventStream → Refresh) for game changes
// and from the source's onUpdate callback for async stats, so the ticker only
// backstops missed events — not real-time updates like the old ~1 Hz poll did.
const reconcileInterval = 15 * time.Second

// pregameReconcileInterval is the cadence during agent select. The messaging
// service does relay pregame match changes over the event stream (a live capture
// saw ares-pregame messages on each change), so the stream usually already covers
// allies' picks — but we still poll briskly here as a cheap backstop on the one
// screen where a missed intra-state update is most visible. Agent select is brief,
// so the extra polls are negligible.
const pregameReconcileInterval = 1500 * time.Millisecond

// Sink is how the picker emits frames toward the phone (via the relay).
type Sink interface {
	SendState(State)
	SendResult(reqID string, ok bool, message string)
	SendAuthStatus(ok bool, message string)
}

// Picker owns the poll loop, derives wire state from the game (source of truth),
// and applies phone intents. Live state is always reconciled against the next
// poll; only our intents (config) are authoritative locally.
type Picker struct {
	cfg  *config.Config
	src  Source
	sink Sink

	refresh chan struct{}

	mu              sync.Mutex
	last            State
	hasLast         bool
	matchID         string
	partyID         string // current party id, for routing party commands
	partyOwner      bool   // whether we own the party (gates owner-only commands)
	optAgent        string // optimistic lock target, awaiting reconcile
	optSince        time.Time
	autoLockedMatch string // match we've already auto-locked in (fire once per match)
}

// New builds a picker over a game source, emitting through sink.
func New(cfg *config.Config, src Source, sink Sink) *Picker {
	p := &Picker{cfg: cfg, src: src, sink: sink, refresh: make(chan struct{}, 1)}
	// If the source reports async updates (live: background stats fetches), wire
	// them to a refresh so freshly-loaded rows reach the phone immediately. The
	// sim and test fakes don't implement this and just keep the tick cadence.
	if u, ok := src.(interface{ SetOnUpdate(func()) }); ok {
		u.SetOnUpdate(p.triggerRefresh)
	}
	return p
}

// Run polls immediately (cold-start: render whatever the game is doing now),
// then reconciles on the slow ticker as a safety net while the local event
// stream and phone commands drive prompt re-polls on demand via Refresh.
func (p *Picker) Run(ctx context.Context) {
	p.poll(ctx)
	t := time.NewTimer(p.reconcileDelay())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.poll(ctx)
			t.Reset(p.reconcileDelay())
		case <-p.refresh:
			p.poll(ctx)
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(p.reconcileDelay())
		}
	}
}

// reconcileDelay picks the safety-net poll cadence for the current state: brisk
// during agent select (where allies' GLZ-side picks may not push over the event
// stream), relaxed elsewhere (where presence/messaging events drive refreshes).
func (p *Picker) reconcileDelay() time.Duration {
	p.mu.Lock()
	st := p.last.State
	p.mu.Unlock()
	if st == "pregame" || st == "locked" {
		return pregameReconcileInterval
	}
	return reconcileInterval
}

// Republish re-sends the last known state. Call it when a relay connection is
// (re)established so a freshly-connected phone gets a snapshot even if the game
// state hasn't changed since startup.
func (p *Picker) Republish() {
	p.mu.Lock()
	st, ok := p.last, p.hasLast
	p.mu.Unlock()
	if ok {
		p.sink.SendState(st)
	}
}

func (p *Picker) triggerRefresh() {
	select {
	case p.refresh <- struct{}{}:
	default:
	}
}

// Refresh triggers an immediate state re-poll. Safe for concurrent use and
// coalesces with the ticker and any pending refresh, so the local event stream
// can fire it freely on every relevant game change without piling up polls.
func (p *Picker) Refresh() { p.triggerRefresh() }

func (p *Picker) poll(ctx context.Context) {
	snap, err := p.src.Snapshot(ctx)
	if err != nil {
		log.Printf("picker: snapshot error: %v", err)
	}
	p.mu.Lock()
	p.disarmOnMatchEnd(snap, err)
	st := p.build(snap, err)
	changed := !p.hasLast || !reflect.DeepEqual(st, p.last)
	p.last, p.hasLast = st, true
	p.matchID = snap.MatchID
	p.partyID, p.partyOwner = snap.PartyID, snap.IsOwner
	fire, agent, mid := p.planAutoLock(snap.MatchID, st)
	p.mu.Unlock()
	if changed {
		p.sink.SendState(st)
	}
	if fire {
		go p.doAutoLock(ctx, mid, agent)
	}
}

// inMatch reports whether a wire state is one of the in-match phases (agent
// select through the live game). Used to detect the match-end edge.
func inMatch(state string) bool {
	return state == "pregame" || state == "locked" || state == "ingame"
}

// disarmOnMatchEnd clears the armed pre-pick once, on the transition from an
// in-match phase back to a pre-match menu/lobby screen, so an arm is one-shot:
// it never carries into the next match (the user re-arms each game). The
// auto-lock toggle is a persistent preference and is left untouched. Caller
// must hold p.mu. Restricted to a clean return to the menus (not offline/error)
// so a transient socket blip mid-game can't wipe a still-valid arm.
func (p *Picker) disarmOnMatchEnd(snap Snapshot, err error) {
	if !p.hasLast || !inMatch(p.last.State) {
		return // wasn't in a match → no match-end edge to act on
	}
	if err != nil || !snap.Running {
		return // offline/error could be transient; keep the arm
	}
	switch snap.Phase {
	case "menus", "lobby", "queue", "matchfound", "":
		// back at a pre-match screen → the match (or dodge) is over
	default:
		return // still pregame/ingame → not yet a match-end edge
	}
	if p.cfg.PrepickAgentUUID == "" {
		return // nothing armed
	}
	log.Printf("picker: match ended; disarming pre-pick %s", p.cfg.PrepickAgentUUID)
	p.cfg.PrepickAgentUUID = ""
	if e := p.cfg.Save(); e != nil {
		log.Printf("picker: save after disarm: %v", e)
	}
}

// planAutoLock decides whether to fire the armed pre-pick. Caller must hold p.mu.
// It commits (marks the match fired + opens the optimistic window) before
// returning true so overlapping polls can't double-fire. Decision 5: calm —
// fires once per match at the poll cadence, never on taken/unowned agents.
func (p *Picker) planAutoLock(matchID string, st State) (fire bool, agent, mid string) {
	agent = p.cfg.PrepickAgentUUID
	switch {
	case st.State != "pregame":
		return false, "", "" // only locks in agent select
	case !p.cfg.Enabled || !p.cfg.AutoLock || agent == "":
		return false, "", "" // disarmed
	case st.SelfStatus == "locked":
		return false, "", "" // already locked (by us, the PC, or a prior fire)
	case p.autoLockedMatch == matchID:
		return false, "", "" // already fired this match
	case p.optAgent != "":
		return false, "", "" // an attempt is already in flight
	case contains(st.TakenAgentUUIDs, agent):
		return false, "", "" // taken → notify + manual (no auto-lock)
	case len(st.OwnedAgentUUIDs) > 0 && !contains(st.OwnedAgentUUIDs, agent):
		return false, "", "" // not owned → can't lock
	}
	p.autoLockedMatch = matchID
	p.optAgent, p.optSince = agent, time.Now()
	return true, agent, matchID
}

// doAutoLock performs the select+lock off the poll loop. It does not retry: a
// failure leaves the match marked fired (calm — won't spam), and the optimistic
// window times out back to "armed" so the user can lock manually.
func (p *Picker) doAutoLock(ctx context.Context, matchID, agent string) {
	log.Printf("picker: auto-locking %s in %s", agent, matchID)
	if err := p.src.Select(ctx, matchID, agent); err != nil {
		log.Printf("picker: auto-lock select: %v", err)
		return
	}
	if err := p.src.Lock(ctx, matchID, agent); err != nil {
		log.Printf("picker: auto-lock lock: %v", err)
		return
	}
	p.triggerRefresh()
}

// build derives the wire State from a snapshot. Caller must hold p.mu (it reads
// and reconciles the optimistic-lock fields).
func (p *Picker) build(snap Snapshot, err error) State {
	st := State{
		AutoLock:         p.cfg.AutoLock,
		Enabled:          p.cfg.Enabled,
		PrepickAgentUUID: p.cfg.PrepickAgentUUID,
		OwnedAgentUUIDs:  orEmpty(snap.OwnedAgents),
		TakenAgentUUIDs:  []string{},
		Teammates:        []Teammate{},
		MatchPlayers:     []MatchSeat{},
		PartyMembers:     []PartyMember{},
		PrepickStatus:    "none",
		GameLocale:       snap.Locale,
	}
	switch {
	case err != nil:
		st.State = "error"
		return st
	case !snap.Running:
		st.State = "offline"
		return st
	}

	switch snap.Phase {
	case "ingame":
		st.State = "ingame"
		st.ScoreAlly, st.ScoreEnemy, st.ScoreValid = snap.ScoreAlly, snap.ScoreEnemy, snap.HasScore
		puuid := p.src.PUUID()
		for _, pl := range snap.Players {
			st.MatchPlayers = append(st.MatchPlayers, MatchSeat{
				Name:       pl.Name,
				AgentUUID:  pl.CharacterID,
				Team:       pl.Team,
				Self:       pl.PUUID == puuid,
				Stats:      pl.Stats,
				PartyGroup: pl.PartyGroup,
			})
		}
	case "pregame":
		st.MatchID = snap.MatchID
		st.MapID = snap.MapID
		st.QueueID = snap.QueueID
		st.PhaseTimeRemainingNS = snap.PhaseTimeRemainingNS

		puuid := p.src.PUUID()
		var ourState, ourAgent string
		taken := []string{}
		for _, pl := range snap.Players {
			self := pl.PUUID == puuid
			st.Teammates = append(st.Teammates, Teammate{
				Name:       pl.Name,
				AgentUUID:  pl.CharacterID,
				Status:     pl.SelectionState,
				Self:       self,
				Stats:      pl.Stats,
				PartyGroup: pl.PartyGroup,
			})
			if self {
				ourState, ourAgent = pl.SelectionState, pl.CharacterID
			} else if pl.SelectionState == "locked" && pl.CharacterID != "" {
				taken = append(taken, pl.CharacterID)
			}
		}
		st.TakenAgentUUIDs = taken
		st.SelfAgentUUID = ourAgent
		st.SelfStatus = ourState
		st.PrepickStatus = p.derivePrepickStatus(ourState, ourAgent, taken)
		if ourState == "locked" {
			st.State = "locked"
		} else {
			st.State = "pregame"
		}
	default:
		// Pre-match: menus|lobby|queue|matchfound (source already classified it).
		st.State = snap.Phase
		if st.State == "" {
			st.State = "menus"
		}
		st.QueueID = snap.QueueID
		p.optAgent = "" // any in-flight lock is moot outside agent select
		st.PrepickStatus = p.derivePrepickStatus("", "", nil)

		// Party (lobby) surface for the phone's drawer.
		st.PartyID = snap.PartyID
		st.PartyAccessibility = snap.Accessibility
		st.PartyInviteCode = snap.InviteCode
		st.PartyMaxSize = snap.MaxPartySize
		st.IsPartyOwner = snap.IsOwner
		st.QueueEntryTime = snap.QueueEntryMillis
		for _, m := range snap.PartyMembers {
			st.PartyMembers = append(st.PartyMembers, PartyMember{
				PUUID: m.PUUID, Name: m.Name, IsOwner: m.IsOwner, IsReady: m.IsReady, Self: m.Self, Stats: m.Stats,
			})
		}
	}
	return st
}

// derivePrepickStatus is the pre-pick lifecycle as seen by the phone. Game truth
// wins: we never settle to "locked" from our own POST — only the game's
// CharacterSelectionState does. Order: a confirmed lock, then an in-flight
// optimistic attempt (locking / settled to taken / timed out), then the resting
// armed-vs-taken view of the configured pre-pick.
func (p *Picker) derivePrepickStatus(ourState, ourAgent string, taken []string) string {
	if ourState == "locked" {
		if p.optAgent != "" && ourAgent == p.optAgent {
			p.optAgent = ""
		}
		return "locked"
	}
	if p.optAgent != "" {
		switch {
		case contains(taken, p.optAgent):
			p.optAgent = ""
			return "taken"
		case time.Since(p.optSince) > 5*time.Second:
			p.optAgent = "" // give up; fall through to the resting view
		default:
			return "locking"
		}
	}
	if p.cfg.PrepickAgentUUID != "" {
		if contains(taken, p.cfg.PrepickAgentUUID) {
			return "taken" // armed pick already grabbed by an ally
		}
		return "armed"
	}
	return "none"
}

// HandlePhoneFrame applies a phone→PC command. Safe to call concurrently; it may
// block on Riot HTTP, so callers should invoke it in its own goroutine.
func (p *Picker) HandlePhoneFrame(ctx context.Context, typ, reqID string, data json.RawMessage) {
	switch typ {
	case "get_state":
		p.mu.Lock()
		st, ok := p.last, p.hasLast
		p.mu.Unlock()
		if ok {
			p.sink.SendState(st)
		}

	case "select":
		agent := agentID(data)
		mid := p.currentMatch()
		if mid == "" || agent == "" {
			p.sink.SendResult(reqID, false, "not in agent select")
			return
		}
		if err := p.src.Select(ctx, mid, agent); err != nil {
			p.sink.SendResult(reqID, false, err.Error())
			return
		}
		p.sink.SendResult(reqID, true, "selected")
		p.triggerRefresh()

	case "lock":
		agent := agentID(data)
		mid := p.currentMatch()
		if mid == "" || agent == "" {
			p.sink.SendResult(reqID, false, "not in agent select")
			return
		}
		// Lock is select-then-lock (instalock pattern from the reference).
		if err := p.src.Select(ctx, mid, agent); err != nil {
			p.sink.SendResult(reqID, false, err.Error())
			return
		}
		if err := p.src.Lock(ctx, mid, agent); err != nil {
			p.sink.SendResult(reqID, false, err.Error())
			return
		}
		p.mu.Lock()
		p.optAgent, p.optSince = agent, time.Now()
		p.mu.Unlock()
		p.sink.SendResult(reqID, true, "locking")
		p.triggerRefresh()

	case "set_config":
		var d struct {
			Enabled          *bool   `json:"enabled"`
			AutoLock         *bool   `json:"auto_lock"`
			PrepickAgentUUID *string `json:"prepick_agent_uuid"`
		}
		_ = json.Unmarshal(data, &d)
		if d.Enabled != nil {
			p.cfg.Enabled = *d.Enabled
		}
		if d.AutoLock != nil {
			p.cfg.AutoLock = *d.AutoLock
		}
		if d.PrepickAgentUUID != nil {
			p.cfg.PrepickAgentUUID = *d.PrepickAgentUUID
		}
		if err := p.cfg.Save(); err != nil {
			log.Printf("picker: save config: %v", err)
		}
		p.sink.SendResult(reqID, true, "config updated")
		p.triggerRefresh()

	case "test_auth":
		if err := p.src.Authenticate(ctx); err != nil {
			p.sink.SendAuthStatus(false, err.Error())
			return
		}
		p.sink.SendAuthStatus(true, "riot auth ok")
		p.triggerRefresh()

	default:
		// Party (lobby) management commands are namespaced party_*; everything
		// else (e.g. a legacy ping) is an unknown frame and ignored.
		if strings.HasPrefix(typ, "party_") {
			p.handlePartyFrame(ctx, typ, reqID, data)
		}
	}
}

// handlePartyFrame applies a party_* (lobby) command. Split out of
// HandlePhoneFrame so the core picker commands and the lobby-management surface
// stay separately legible. Owner-only commands gate on ownerParty first and
// never issue the Riot call when we don't own the party (the API rejects them
// anyway — honest UX + defense in depth). join/leave are open to any member.
func (p *Picker) handlePartyFrame(ctx context.Context, typ, reqID string, data json.RawMessage) {
	switch typ {
	case "party_generate_code":
		pid, ok := p.ownerParty(reqID)
		if !ok {
			return
		}
		p.partyAction(reqID, "invite code generated", func() error { return p.src.GenerateInviteCode(ctx, pid) })

	case "party_disable_code":
		pid, ok := p.ownerParty(reqID)
		if !ok {
			return
		}
		p.partyAction(reqID, "invite code disabled", func() error { return p.src.DisableInviteCode(ctx, pid) })

	case "party_join_by_code":
		var d struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(data, &d)
		code := strings.ToUpper(strings.TrimSpace(d.Code))
		if code == "" {
			p.sink.SendResult(reqID, false, "no code")
			return
		}
		p.partyAction(reqID, "joined party", func() error { return p.src.JoinByCode(ctx, code) })

	case "party_leave":
		p.partyAction(reqID, "left party", func() error { return p.src.LeaveParty(ctx) })

	case "party_kick":
		_, owner := p.currentParty()
		if !p.requireOwner(reqID, owner) {
			return
		}
		var d struct {
			PUUID string `json:"puuid"`
		}
		_ = json.Unmarshal(data, &d)
		if d.PUUID == "" {
			p.sink.SendResult(reqID, false, "no player")
			return
		}
		p.partyAction(reqID, "removed from party", func() error { return p.src.KickMember(ctx, d.PUUID) })

	case "party_set_accessibility":
		pid, ok := p.ownerParty(reqID)
		if !ok {
			return
		}
		var d struct {
			Accessibility string `json:"accessibility"`
		}
		_ = json.Unmarshal(data, &d)
		open := strings.EqualFold(d.Accessibility, "OPEN")
		p.partyAction(reqID, "party updated", func() error { return p.src.SetAccessibility(ctx, pid, open) })

	case "party_set_queue":
		pid, ok := p.ownerParty(reqID)
		if !ok {
			return
		}
		var d struct {
			QueueID string `json:"queueId"`
		}
		_ = json.Unmarshal(data, &d)
		if d.QueueID == "" {
			p.sink.SendResult(reqID, false, "no queue")
			return
		}
		p.partyAction(reqID, "queue set", func() error { return p.src.ChangeQueue(ctx, pid, d.QueueID) })

	case "party_start_matchmaking":
		pid, ok := p.ownerParty(reqID)
		if !ok {
			return
		}
		p.partyAction(reqID, "searching for a match", func() error { return p.src.StartMatchmaking(ctx, pid) })

	case "party_stop_matchmaking":
		pid, ok := p.ownerParty(reqID)
		if !ok {
			return
		}
		p.partyAction(reqID, "search cancelled", func() error { return p.src.StopMatchmaking(ctx, pid) })
	}
}

func (p *Picker) currentMatch() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.matchID
}

// currentParty returns the current party id and whether we own it (last poll).
func (p *Picker) currentParty() (id string, owner bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.partyID, p.partyOwner
}

// requireParty fails the request if we don't have a party id yet.
func (p *Picker) requireParty(reqID, pid string) bool {
	if pid == "" {
		p.sink.SendResult(reqID, false, "not in a party")
		return false
	}
	return true
}

// requireOwner fails the request if we don't own the party (owner-only command).
func (p *Picker) requireOwner(reqID string, owner bool) bool {
	if !owner {
		p.sink.SendResult(reqID, false, "only the party owner can do that")
		return false
	}
	return true
}

// ownerParty returns the current party id when we both have a party and own it —
// the gate every owner-only, party-scoped command shares. ok=false means a
// failure reply was already sent and the caller should return.
func (p *Picker) ownerParty(reqID string) (pid string, ok bool) {
	id, owner := p.currentParty()
	if !p.requireParty(reqID, id) || !p.requireOwner(reqID, owner) {
		return "", false
	}
	return id, true
}

// partyAction runs a party Riot call, replies ok/err to the phone, and triggers a
// refresh so the next pushed state reflects the change (the Party read is truth).
func (p *Picker) partyAction(reqID, okMsg string, fn func() error) {
	if err := fn(); err != nil {
		p.sink.SendResult(reqID, false, err.Error())
		return
	}
	p.sink.SendResult(reqID, true, okMsg)
	p.triggerRefresh()
}

func agentID(data json.RawMessage) string {
	var d struct {
		AgentID string `json:"agentId"`
	}
	_ = json.Unmarshal(data, &d)
	return d.AgentID
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
