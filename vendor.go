// Utilities for dealing with vendor directories and the modules.txt

package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

func findModuleRoot(dir string) (root string) {
	if dir == "" {
		log.Fatal("dir not set")
	}

	dir = filepath.Clean(dir)

	// Look for enclosing go.mod.
	for {
		if fi, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !fi.IsDir() {
			return dir
		}
		d := filepath.Dir(dir)
		if d == dir {
			break
		}
		dir = d
	}

	return ""
}

func loadModFile(filename string) *modfile.File {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	file, err := modfile.Parse(filename, data, nil)
	if err != nil {
		panic(err)
	}

	return file
}

func moduleLine(m, r module.Version) string {
	b := new(strings.Builder)
	b.WriteString("# ")
	b.WriteString(m.Path)
	if m.Version != "" {
		b.WriteString(" ")
		b.WriteString(m.Version)
	}
	if r.Path != "" {
		b.WriteString(" => ")
		b.WriteString(r.Path)
		if r.Version != "" {
			b.WriteString(" ")
			b.WriteString(r.Version)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func stubModulesTxt() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Unable to load current directory: %v", err)
	}

	modRoot := findModuleRoot(wd)

	if modRoot == "" {
		// no go.mod was found, so we don't need to stub modules.txt
	}

	modFile := loadModFile(filepath.Join(modRoot, "go.mod"))

	vdir := filepath.Join(modRoot, "vendor")

	if gv := modFile.Go; gv != nil && semver.Compare("v"+gv.Version, "v1.14") >= 0 {
		// If the Go version is at least 1.14, generate a dummy modules.txt using only the information
		// in the go.mod file

		generated := make(map[module.Version]bool)
		var buf bytes.Buffer
		for _, r := range modFile.Require {
			// TODO: support replace lines
			generated[r.Mod] = true
			line := moduleLine(r.Mod, module.Version{})
			buf.WriteString(line)

			buf.WriteString("## explicit\n")

			buf.WriteString(r.Mod.Path + "\n")
		}

		// Record unused and wildcard replacements at the end of the modules.txt file:
		// without access to the complete build list, the consumer of the vendor
		// directory can't otherwise determine that those replacements had no effect.
		for _, r := range modFile.Replace {
			if generated[r.Old] {
				// We we already recorded this replacement in the entry for the replaced
				// module with the packages it provides.
				continue
			}

			line := moduleLine(r.Old, r.New)
			buf.WriteString(line)
		}

		if buf.Len() == 0 {
			log.Println("go: no dependencies to vendor")
			return
		}

		if err := os.MkdirAll(vdir, 0777); err != nil {
			log.Fatalf("go mod vendor: %v", err)
		}

		if err := ioutil.WriteFile(filepath.Join(vdir, "modules.txt"), buf.Bytes(), 0666); err != nil {
			log.Fatalf("go mod vendor: %v", err)
		}
	}
}
