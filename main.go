// outrash finds the caches of developer tools and everyday apps and shows what
// can be rebuilt. The checked rows are the plan; the window only deletes when
// the user presses the button that confirms them.
package main

import (
	"flag"
	"log"
	"os"

	"github.com/egoist/mygo"

	"outrash/internal/app"
	"outrash/internal/core"
)

func main() {
	debug := flag.Bool("debug", false, "list plans on stdout and never delete")
	noDelete := flag.Bool("no-delete", false, "refuse every deletion, keeping the window's plan view")
	flag.Parse()

	cfg, err := core.Parse(core.Rules)
	if err != nil {
		log.Fatal(err)
	}

	mygo.App.SetName("outrash")
	// The appearance the last run chose, else the system's. It also decides
	// what the platform paints its own chrome — caption glyphs, traffic
	// lights — with, so it is set before the window exists.
	app.LoadTheme()
	app.LoadLang()
	mygo.App.WhenReady(func() {
		// The first paint should not flash the other appearance while the
		// frame builds: the ground matches what the window is about to wear.
		backgroundColor := "#F1F4F8"
		if mygo.Theme.IsDark() {
			backgroundColor = "#0D1117"
		}
		a := app.New(cfg, core.DefaultOptions(), app.Options{Debug: *debug, NoDelete: *noDelete || *debug})
		a.Window = mygo.NewWindow(mygo.WindowOptions{
			Title:           "outrash",
			Width:           1360,
			Height:          820,
			MinWidth:        1000,
			MinHeight:       640,
			BackgroundColor: backgroundColor,
			TitleBarStyle:   mygo.TitleBarHidden,
			Content:         app.View(a),
		})
		a.Scan()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}
