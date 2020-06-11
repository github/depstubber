package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"runtime/debug"
	"strings"

	"golang.org/x/tools/go/packages"
)

// removeDot removes a dot from the end of `s`, if it ends with a dot.
func removeDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[0 : len(s)-1]
	}
	return s
}

// packageNameOfDir get package import path via dir
func packageNameOfDir(srcDir string) (string, error) {
	files, err := ioutil.ReadDir(srcDir)
	if err != nil {
		log.Fatal(err)
	}

	var goFilePath string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".go") {
			goFilePath = file.Name()
			break
		}
	}
	if goFilePath == "" {
		return "", fmt.Errorf("go source file not found %s", srcDir)
	}

	packageImport, err := parsePackageImport(goFilePath, srcDir)
	if err != nil {
		return "", err
	}
	return packageImport, nil
}

func printModuleVersion() {
	if bi, exists := debug.ReadBuildInfo(); exists {
		fmt.Println(bi.Main.Version)
	} else {
		log.Printf("No version information found. Make sure to use " +
			"GO111MODULE=on when running 'go get' in order to use specific " +
			"version of the binary.")
	}
}

// parseImportPackage get package import path via source file
func parsePackageImport(source, srcDir string) (string, error) {
	cfg := &packages.Config{
		Mode:  packages.NeedName,
		Tests: true,
		Dir:   srcDir,
	}
	pkgs, err := packages.Load(cfg, "file="+source)
	if err != nil {
		return "", err
	}
	if packages.PrintErrors(pkgs) > 0 || len(pkgs) == 0 {
		return "", errors.New("loading package failed")
	}

	packageImport := pkgs[0].PkgPath

	// It is illegal to import a _test package.
	packageImport = strings.TrimSuffix(packageImport, "_test")
	return packageImport, nil
}

func split(s string) []string {
	return strings.FieldsFunc(s, func(c rune) bool { return c == ',' })
}
