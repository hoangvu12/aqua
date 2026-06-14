// Command wsprobe connects to the local Riot Client websocket and prints every
// event it emits, tagging the ones aqua's picker reacts to with [MATCH]. Run it
// with the Riot Client / VALORANT open to verify the event protocol live and see
// the real resource URIs the *current* client uses — the version-sensitive bit
// (e.g. the chat presence version, /chat/v4 vs /chat/v6) that the 2022 reference
// loggers may have gotten wrong.
//
//	go -C pc run ./cmd/wsprobe
//
// Then navigate menus → queue → agent select and watch which URIs scroll past.
// Confirm presence events show [MATCH] and note their exact version.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"aqua/internal/riot"
)

func main() {
	log.SetFlags(log.Ltime)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Println("wsprobe: connecting to the local Riot Client websocket (open VALORANT)… Ctrl+C to quit")
	es := &riot.EventStream{OnEvent: func(ev riot.Event) {
		tag := "       "
		if riot.IsMatchRelevant(ev.URI) {
			tag = "[MATCH]"
		}
		log.Printf("%s %-7s %s", tag, ev.Type, ev.URI)
	}}
	es.Run(ctx) // returns on Ctrl+C
	log.Println("wsprobe: bye")
}
