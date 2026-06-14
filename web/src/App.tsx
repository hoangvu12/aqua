import { useCallback, useEffect, useMemo, useState } from "react";
import { useRelay } from "@/lib/relay";
import { useCatalog } from "@/lib/use-catalog";
import { agentByUuid, mapByMapId, rolesOf } from "@/lib/catalog";
import {
  clearCreds,
  loadCreds,
  readPairLink,
  type Creds,
} from "@/lib/pair";
import {
  hasManualLang,
  persistLang,
  resolveLang,
  type Lang,
} from "@/lib/i18n";
import { loadRecents, pushRecent } from "@/lib/recents";
import { StatusBar } from "@/components/StatusBar";
import { RoleFilter } from "@/components/RoleFilter";
import { AgentGrid } from "@/components/AgentGrid";
import { AlliesStrip } from "@/components/AlliesStrip";
import { ActionBar } from "@/components/ActionBar";
import { PartyBar } from "@/components/PartyBar";
import { PartyDrawer } from "@/components/PartyDrawer";
import { Pairing } from "@/components/Pairing";
import { StateScreen } from "@/components/StateScreen";
import { Scoreboard } from "@/components/Scoreboard";
import { AppShell } from "@/components/AppShell";

const CONTROLLER_STATES = new Set([
  "menus",
  "lobby",
  "queue",
  "matchfound",
  "pregame",
  "locked",
]);

// Pre-match states own the party (lobby) surface; pregame/locked switch the strip
// slot back to the read-only allies view.
const PREMATCH_STATES = new Set(["menus", "lobby", "queue", "matchfound"]);

export default function App() {
  const [creds, setCreds] = useState<Creds | null>(() => loadCreds());
  const pairLink = useMemo(() => readPairLink(), []);
  const [lang, setLang] = useState<Lang>(() => resolveLang(undefined));

  const onAuthInvalid = useCallback(() => {
    clearCreds();
    setCreds(null);
  }, []);

  const relay = useRelay(creds, onAuthInvalid);
  const game = relay.game;
  const { catalog } = useCatalog(game?.game_locale);

  // Local UI intent (reconciled against pushed game state).
  const [selectedUuid, setSelectedUuid] = useState<string | null>(null);
  const [roleFilter, setRoleFilter] = useState<string | null>(null);
  const [pendingLock, setPendingLock] = useState(false);
  const [recents, setRecents] = useState<string[]>(loadRecents);
  const [partyOpen, setPartyOpen] = useState(false);

  // The party drawer only belongs to pre-match states; close it on any exit so it
  // can't linger over agent select. Also close the moment a match is found — the
  // ready-check takes over and there's nothing left to manage.
  const preMatch = !!game && PREMATCH_STATES.has(game.state);
  useEffect(() => {
    if (!preMatch || game?.state === "matchfound") setPartyOpen(false);
  }, [preMatch, game?.state]);

  // Re-resolve language when the game locale arrives, unless the user chose one.
  useEffect(() => {
    if (!hasManualLang() && game?.game_locale) setLang(resolveLang(game.game_locale));
  }, [game?.game_locale]);

  // Fresh selection per match.
  useEffect(() => setSelectedUuid(null), [game?.match_id]);

  // Clear optimistic lock once the game settles (locked / taken), with a safety timeout.
  useEffect(() => {
    if (game?.self_status === "locked" || game?.prepick_status === "taken") setPendingLock(false);
  }, [game?.self_status, game?.prepick_status]);
  useEffect(() => {
    if (!pendingLock) return;
    const id = setTimeout(() => setPendingLock(false), 6000);
    return () => clearTimeout(id);
  }, [pendingLock]);

  // "PC offline" grace: connected to the relay but no state pushed yet.
  const [graceExpired, setGraceExpired] = useState(false);
  useEffect(() => {
    if (relay.gotState) {
      setGraceExpired(false);
      return;
    }
    const id = setTimeout(() => setGraceExpired(true), 3000);
    return () => clearTimeout(id);
  }, [relay.gotState]);

  const toggleLang = useCallback(() => {
    setLang((prev) => {
      const next: Lang = prev === "vi" ? "en" : "vi";
      persistLang(next);
      return next;
    });
  }, []);

  const roles = useMemo(() => rolesOf(catalog), [catalog]);

  // ── Pairing gate ───────────────────────────────────────────────────────────
  if (!creds) {
    return (
      <AppShell>
        <Pairing pairLink={pairLink} onPaired={setCreds} lang={lang} onToggleLang={toggleLang} />
      </AppShell>
    );
  }

  // ── Effective selection (local intent, falling back to game truth) ──────────
  let effectiveUuid: string | null = null;
  if (game) {
    if (game.state === "pregame") effectiveUuid = selectedUuid ?? (game.self_agent_uuid || null);
    else if (game.state === "locked") effectiveUuid = game.self_agent_uuid || selectedUuid;
    else effectiveUuid = selectedUuid ?? (game.prepick_agent_uuid || null);
  }
  const selectedAgent = effectiveUuid ? agentByUuid(catalog, effectiveUuid) : undefined;

  const onGridSelect = (uuid: string) => {
    setSelectedUuid(uuid);
    if (game?.state === "pregame") relay.select(uuid); // live in-game hover
  };
  const remember = (uuid: string) => setRecents(pushRecent(uuid));
  const onLock = () => {
    if (!effectiveUuid) return;
    setPendingLock(true);
    relay.lock(effectiveUuid);
    remember(effectiveUuid);
  };
  const onDodge = () => relay.dodge();
  const onArm = () => {
    if (!effectiveUuid) return;
    relay.setConfig({ prepick_agent_uuid: effectiveUuid });
    remember(effectiveUuid);
  };
  const onDisarm = () => relay.setConfig({ prepick_agent_uuid: "" });
  const onToggleAutoLock = () => {
    if (game) relay.setConfig({ auto_lock: !game.auto_lock });
  };

  const showController =
    !!game && !!catalog && CONTROLLER_STATES.has(game.state);
  const onScoreboard =
    !!game && game.state === "ingame" && game.match_players.length > 0;

  // Stage backdrop (≥md): the active map carries the wash, the live selection
  // its agent portrait. Absent on pre-match menus, which is fine — the scrim
  // alone still frames the controller.
  const backdropMap = game ? mapByMapId(catalog, game.map_id ?? "") : undefined;

  return (
    <AppShell wide={onScoreboard} agent={selectedAgent} map={backdropMap}>
      <StatusBar
        conn={relay.conn}
        game={game}
        catalog={catalog}
        lang={lang}
        onToggleLang={toggleLang}
      />

      {showController && game ? (
        <>
          <RoleFilter roles={roles} selected={roleFilter} onSelect={setRoleFilter} lang={lang} />
          <div className="no-scrollbar flex-1 overflow-y-auto">
            <AgentGrid
              agents={catalog!.agents}
              game={game}
              roleFilter={roleFilter}
              recents={recents}
              selectedUuid={effectiveUuid}
              onSelect={onGridSelect}
              lang={lang}
            />
          </div>
          {preMatch && game.party_id ? (
            <PartyBar
              game={game}
              catalog={catalog}
              lang={lang}
              onOpen={() => setPartyOpen(true)}
              onCancelSearch={() => relay.party.stopMatchmaking()}
            />
          ) : (
            game.teammates.length > 0 && (
              <AlliesStrip teammates={game.teammates} catalog={catalog} lang={lang} />
            )
          )}
          <ActionBar
            game={game}
            selectedAgent={selectedAgent}
            lang={lang}
            pendingLock={pendingLock}
            onLock={onLock}
            onDodge={onDodge}
            onArm={onArm}
            onDisarm={onDisarm}
            onToggleAutoLock={onToggleAutoLock}
          />
          {preMatch && game.party_id && (
            <PartyDrawer
              open={partyOpen}
              onClose={() => setPartyOpen(false)}
              game={game}
              catalog={catalog}
              lang={lang}
              party={relay.party}
            />
          )}
        </>
      ) : game && game.state === "ingame" && game.match_players.length > 0 ? (
        <Scoreboard
          players={game.match_players}
          score={game.score_valid ? { ally: game.score_ally, enemy: game.score_enemy } : null}
          catalog={catalog}
          lang={lang}
        />
      ) : (
        <StateScreen kind={screenKind(game, graceExpired)} lang={lang} />
      )}
    </AppShell>
  );
}

function screenKind(
  game: ReturnType<typeof useRelay>["game"],
  graceExpired: boolean,
): "loading" | "offline" | "error" | "ingame" {
  if (!game) return graceExpired ? "offline" : "loading";
  if (game.state === "offline") return "offline";
  if (game.state === "error") return "error";
  if (game.state === "ingame") return "ingame";
  return "loading"; // controller state but catalog not ready yet
}
