package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
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

func DirExists(path string) (bool, error) {
	return FileExists(path)
}

func FileExists(filepath string) (bool, error) {
	_, err := os.Stat(filepath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err == nil {
		return true, nil
	}
	return false, err
}

// CreateFolderIfNotExists creates a folder if it does not exists.
func CreateFolderIfNotExists(name string, perm os.FileMode) error {
	_, err := os.Stat(name)
	if os.IsNotExist(err) {
		return os.MkdirAll(name, perm)
	}
	return err
}

func MustCreateFolderIfNotExists(path string, perm os.FileMode) {
	err := CreateFolderIfNotExists(path, perm)
	if err != nil {
		panic(fmt.Sprintf("error creating dir %q: %s", path, err))
	}
}

func MustCopyFile(src, dst string) {
	_, err := copyFile(src, dst)
	if err != nil {
		log.Fatalf("error copying %q to %q: %s", src, dst, err)
	}
}

func copyFile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}
