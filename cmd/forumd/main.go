package main

import (
	"log"
	"net/http"
	"os"

	// note-to-self: import my internal app package from my own module.
	// I alias it to "app" so the call sites read nicely: app.New(), a.Router()
	app "gitea.kood.tech/mohammadtavasoli/Literary-lions/internal"
)

func main() {
	// note-to-self: read PORT from env, default to 8080 for local dev
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// note-to-self: build my App (templates + routes)
	a, err := app.New()
	if err != nil {
		log.Fatal(err) // if templates fail to parse, crash fast
	}

	log.Printf("listening on :%s", port)

	// note-to-self: hand off to the HTTP server using my router
	// (CTRL+C in terminal to stop)
	if err := http.ListenAndServe(":"+port, a.Router()); err != nil {
		log.Fatal(err)
	}
}
