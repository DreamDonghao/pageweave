// releaseplan is a CI helper; it is not included in the service image.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/DreamDonghao/pageweave/internal/releaseplan"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 6 {
		return fmt.Errorf("usage: releaseplan TAG VERSION SHA PRERELEASE RELEASES_JSON")
	}
	data, e := os.ReadFile(os.Args[5])
	if e != nil {
		return e
	}
	var pages [][]releaseplan.Release
	if e = json.Unmarshal(data, &pages); e != nil {
		return e
	}
	var releases []releaseplan.Release
	for _, p := range pages {
		releases = append(releases, p...)
	}
	tags, e := releaseplan.Tags(os.Args[1], os.Args[2], os.Args[3], os.Args[4] == "true", releases)
	if e != nil {
		return e
	}
	fmt.Println(strings.Join(tags, "\n"))
	return nil
}
