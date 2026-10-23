// outrash finds the dependency caches of developer tools and shows what can
// be rebuilt. It never deletes anything itself: every action opens a plan
// of exact paths, sizes and order for the user to read and confirm.
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
	// The window's own caption buttons are painted from the system
	// appearance, which on a dark desktop is white glyphs on this near-white
	// bar. Locking the appearance light also locks them to dark ink.
	mygo.Theme.SetSource(mygo.ThemeLight)
	mygo.App.WhenReady(func() {
		a := app.New(cfg, core.DefaultOptions(), app.Options{Debug: *debug, NoDelete: *noDelete || *debug})
		a.Window = mygo.NewWindow(mygo.WindowOptions{
			Title:           "outrash",
			Width:           1200,
			Height:          780,
			MinWidth:        980,
			MinHeight:       620,
			BackgroundColor: "#F6F6F4",
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
