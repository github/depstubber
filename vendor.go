// Utilities for dealing with vendor directories and the modules.txt

package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
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
		return
	}

	modFile := loadModFile(filepath.Join(modRoot, "go.mod"))
	vdir := filepath.Join(modRoot, "vendor")

	if gv := modFile.Go; gv != nil && semver.Compare("v"+gv.Version, "v1.14") >= 0 {
		// Find imports from all Go files in the project
		usedPackages := findPackagesInSourceCode(modRoot)

		generated := make(map[module.Version]bool)
		var buf bytes.Buffer
		for _, r := range modFile.Require {
			generated[r.Mod] = true
			line := moduleLine(r.Mod, module.Version{})
			buf.WriteString(line)
			buf.WriteString("## explicit\n")

			// List package paths that are used in the source code
			packagesForModule := findPackagesForModule(r.Mod.Path, usedPackages)
			if len(packagesForModule) > 0 {
				for _, pkg := range packagesForModule {
					buf.WriteString(pkg + "\n")
				}
			} else {
				// If we can't find any packages then just list the module path itself
				buf.WriteString(r.Mod.Path + "\n")
			}
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

// findPackagesInSourceCode scans all Go files in the directory tree and extracts import paths
func findPackagesInSourceCode(root string) map[string]bool {
	packages := make(map[string]bool)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip vendor directory and hidden directories
		if info.IsDir() && (info.Name() == "vendor" || strings.HasPrefix(info.Name(), ".")) {
			return filepath.SkipDir
		}

		// Only process Go files
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}

			// Extract import paths from the AST
			for _, imp := range file.Imports {
				pkgPath := strings.Trim(imp.Path.Value, "\"")
				packages[pkgPath] = true
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("Warning: error walking source directory: %v", err)
	}

	return packages
}

// Compile the regular expression once
var majorVersionSuffixRegex = regexp.MustCompile(`^/v[1-9][0-9]*(/|$)`)

// findPackagesForModule returns the submodules of a given module that are actually used in the source code
func findPackagesForModule(modulePath string, usedPackages map[string]bool) []string {
	var packages []string

	for pkg := range usedPackages {
		// Check if this package belongs to the module
		if strings.HasPrefix(pkg, modulePath) {
			// Extract the part after modulePath
			suffix := pkg[len(modulePath):]

			// If `suffix` begins with a major version suffix then we do not have the right module
			// path. For example, if the module path is `example.com/mymodule` and the package path
			// is `example.com/mymodule/v2/submodule` then we should not consider it a match - it
			// is really a match for the module `example.com/mymodule/v2`.
			if !majorVersionSuffixRegex.MatchString(suffix) {
				packages = append(packages, pkg)
			}
		}
	}

	return packages
}
