// outrash finds the dependency caches of developer tools and shows what can
// be rebuilt. It never deletes anything itself: every action opens a plan
// of exact paths, sizes and order for the user to read and confirm.
package main

import (
	_ "embed"
	"flag"
	"log"
	"os"

	"github.com/egoist/mygo"

	"outrash/internal/core"
)

//go:embed rules.json
var rulesJSON []byte

func main() {
	debug := flag.Bool("debug", false, "list plans on stdout and never delete")
	noDelete := flag.Bool("no-delete", false, "refuse every deletion, keeping the window's plan view")
	flag.Parse()

	cfg, err := core.Parse(rulesJSON)
	if err != nil {
		log.Fatal(err)
	}
	a := newApp(cfg, core.DefaultOptions(), options{debug: *debug, noDelete: *noDelete || *debug})

	mygo.App.SetName("outrash")
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:           "outrash",
			Width:           940,
			Height:          660,
			MinWidth:        860,
			MinHeight:       560,
			BackgroundColor: "#F6F6F4",
			TitleBarStyle:   mygo.TitleBarHidden,
			Content:         uiView(a),
		})
		a.scan()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}
