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
	// The window's own caption buttons are painted from the system
	// appearance, which on a dark desktop is white glyphs on this near-white
	// bar. Locking the appearance light also locks them to dark ink.
	mygo.Theme.SetSource(mygo.ThemeLight)
	mygo.App.WhenReady(func() {
		a := app.New(cfg, core.DefaultOptions(), app.Options{Debug: *debug, NoDelete: *noDelete || *debug})
		a.Window = mygo.NewWindow(mygo.WindowOptions{
			Title:           "outrash",
			Width:           1360,
			Height:          820,
			MinWidth:        1000,
			MinHeight:       640,
			BackgroundColor: "#F1F4F8",
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
