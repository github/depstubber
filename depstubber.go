// depstubber generates stub dependencies
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/depstubber/model"
	"github.com/go-enry/go-license-detector/v4/licensedb"
	"github.com/go-enry/go-license-detector/v4/licensedb/api"
	"github.com/go-enry/go-license-detector/v4/licensedb/filer"
	"golang.org/x/tools/imports"
)

var (
	destination    = flag.String("destination", "", "Output file; defaults to stdout.")
	vendor         = flag.Bool("vendor", false, "Set the destination to vendor/<PKGPATH>/stub.go; overrides '-destination'")
	copyrightFile  = flag.String("copyright_file", "", "Copyright file used to add copyright header")
	writeModuleTxt = flag.Bool("write_module_txt", false, "Write a stub modules.txt to get around the go1.14 vendor check, if necessary.")
	forceOverwrite = flag.Bool("force", false, "Delete the destination vendor directory if it already exists.")
)
var (
	modeAutoDetection      = flag.Bool("auto", false, "Automatically detect and stub dependencies of the Go package in the current directory.")
	modePrintGoGenComments = flag.Bool("print", false, "Automatically detect and generate 'go generate' comments for the Go package in the current directory.")
)

func main() {
	flag.Usage = usage
	flag.Parse()

	// if -write_module_txt has been passed, generate a stub version of a `module/vendor.txt` file
	if *writeModuleTxt {
		stubModulesTxt()
		return
	}

	if *modePrintGoGenComments {
		pathToTypeNames, pathToFuncAndVarNames, _, err := autoDetect(".", ".")
		if err != nil {
			log.Fatalf("Error while auto-detecting imported objects: %s", err)
		}
		printGoGenerateComments(pathToTypeNames, pathToFuncAndVarNames)
		return
	}

	if *vendor && *forceOverwrite {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Unable to load current director: %v", err)
		}
		{ // Remove current ./vendor dir if exists:
			vendorDir := filepath.Join(findModuleRoot(wd), "vendor")
			exists, err := DirExists(vendorDir)
			if err != nil {
				panic(err)
			}
			if exists {
				os.RemoveAll(vendorDir)
			}
		}
	}

	if *modeAutoDetection {
		pathToTypeNames, pathToFuncAndVarNames, pathToDirs, err := autoDetect(".", ".")
		if err != nil {
			log.Fatalf("Error while auto-detecting imported objects: %s", err)
		}
		pkgPaths := make([]string, 0)
		{
			for path := range pathToFuncAndVarNames {
				pkgPaths = append(pkgPaths, path)
			}
			for path := range pathToTypeNames {
				pkgPaths = append(pkgPaths, path)
			}
			pkgPaths = DeduplicateStrings(pkgPaths)
			sort.Strings(pkgPaths)
		}

		for _, pkgPath := range pkgPaths {
			createStubs(
				pkgPath,
				pathToTypeNames[pkgPath],
				pathToFuncAndVarNames[pkgPath],
				pathToDirs[pkgPath],
			)
		}
	} else {
		if flag.NArg() != 2 && flag.NArg() != 3 {
			usage()
			log.Fatal("Expected exactly two or three arguments")
		}
		packageName := flag.Arg(0)
		createStubs(packageName, split(flag.Arg(1)), split(flag.Arg(2)), nil)
	}
	if *vendor {
		stubModulesTxt()
	}
}

func createStubs(packageName string, typeNames []string, funcAndVarNames []string, licenseDirs []string) {

	var pkg *model.PackedPkg
	var err error

	if packageName == "." {
		dir, err := os.Getwd()
		if err != nil {
			log.Fatalf("Get current directory failed: %v", err)
		}
		packageName, err = packageNameOfDir(dir)
		if err != nil {
			log.Fatalf("Parse package name failed: %v", err)
		}
	}

	pkg, err = reflectMode(packageName, typeNames, funcAndVarNames)

	if err != nil {
		log.Fatalf("Loading input failed: %v", err)
	}

	dst := os.Stdout
	if *vendor {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Unable to load current director: %v", err)
		}

		*destination = filepath.Join(findModuleRoot(wd), "vendor", packageName, "stub.go")
	}

	if len(*destination) > 0 {
		if err := os.MkdirAll(filepath.Dir(*destination), os.ModePerm); err != nil {
			log.Fatalf("Unable to create directory: %v", err)
		}
		f, err := os.Create(*destination)
		if err != nil {
			log.Fatalf("Failed opening destination file: %v", err)
		}
		defer f.Close()
		dst = f
	}

	g := new(generator)
	g.srcPackage = packageName
	g.srcExports = strings.Join(typeNames, ",")
	g.srcFunctions = strings.Join(funcAndVarNames, ",")

	if *copyrightFile != "" {
		header, err := ioutil.ReadFile(*copyrightFile)
		if err != nil {
			log.Fatalf("Failed reading copyright file: %v", err)
		}

		g.copyrightHeader = string(header)
	} else {
		// check that there is a LICENSE file
	}

	if err := g.Generate(pkg); err != nil {
		log.Fatalf("Failed generating mock: %v", err)
	}
	if _, err := dst.Write(g.Output()); err != nil {
		log.Fatalf("Failed writing to destination: %v", err)
	}

	if licenseDirs != nil {
		for _, licenseSearchDir := range licenseDirs {
			fl, err := filer.FromDirectory(licenseSearchDir)
			if err != nil {
				panic(err)
			}
			licenses, err := licensedb.Detect(fl)
			if err != nil {
				panic(err)
			}

			for _, licenseRelativePath := range gatherFilenames(licenses) {
				// Exclude licenses of vendored packages:
				if strings.Contains(licenseRelativePath, "/vendor/") {
					continue
				}
				licenseFilepath := filepath.Join(licenseSearchDir, licenseRelativePath)

				dstFolder := filepath.Dir(*destination)
				dstFilepath := filepath.Join(dstFolder, licenseRelativePath)
				if strings.HasSuffix(dstFilepath, ".go") {
					// When saving, add .txt extension.
					dstFilepath += ".txt"
				}
				fmt.Println(fmt.Sprintf("Copying %s to %s", licenseFilepath, dstFilepath))

				MustCreateFolderIfNotExists(filepath.Dir(dstFilepath), os.ModePerm)
				MustCopyFile(licenseFilepath, dstFilepath)
			}
		}
	}
}

func gatherFilenames(matches map[string]api.Match) []string {
	res := make([]string, 0)
	for _, v := range matches {
		res = append(res, mapKeys(v.Files)...)
	}

	return DeduplicateStrings(res)
}

func mapKeys(mp map[string]float32) []string {
	res := make([]string, 0)
	for key := range mp {
		res = append(res, key)
	}

	return res
}

func usage() {
	_, _ = io.WriteString(os.Stderr, usageText)
	flag.PrintDefaults()
}

const usageText = `depstubber uses reflection to generate a stub for a library.

It generates stub methods and functions by building a program
that uses reflection. It requires two or three non-flag
arguments: an import path, and a comma-separated list of
symbols, and a comma-separated list of function names.
Examples:
	depstubber database/sql/driver Conn,Driver
	depstubber github.com/Masterminds/squirrel '' Expr

`

type generator struct {
	buf                                  bytes.Buffer
	srcPackage, srcExports, srcFunctions string // may be empty
	copyrightHeader                      string

	packageMap map[string]string // map from import path to package name
}

func (g *generator) p(format string, args ...interface{}) {
	fmt.Fprintf(&g.buf, format+"\n", args...)
}

func (g *generator) Generate(pkg *model.PackedPkg) error {
	g.p("// Code generated by depstubber. DO NOT EDIT.")

	g.p("// This is a simple stub for %s, strictly for use in testing.", g.srcPackage)
	g.p("")

	if g.copyrightHeader != "" {
		g.p("// See the license below for information about the licensing of the original library.")
		g.p("")

		lines := strings.Split(g.copyrightHeader, "\n")
		for _, line := range lines {
			g.p("// %s", line)
		}
		g.p("")
	} else {
		// if no copyright file was specified, assume there is a LICENSE file
		g.p("// See the LICENSE file for information about the licensing of the original library.")
	}

	g.p("// Source: %s (exports: %s; functions: %s)", g.srcPackage, g.srcExports, g.srcFunctions)
	g.p("")

	g.p("")

	g.p(pkg.Body)

	return nil
}

// Output returns the generator's output, formatted in the standard Go style.
func (g *generator) Output() []byte {
	// Format source and add or remove import statements as necessary:
	src, err := imports.Process("", g.buf.Bytes(), nil)
	if err != nil {
		log.Fatalf("Failed to format generated source code: %s\n%s", err, g.buf.String())
	}
	return src
}
