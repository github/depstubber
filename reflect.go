package main

// This file contains the model construction by reflection.

import (
	"bytes"
	"encoding/gob"
	"flag"
	"fmt"
	"go/build"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"text/template"

	"github.com/github/depstubber/model"
)

var (
	progOnly    = flag.Bool("prog_only", false, "Only generate the reflection program; write it to stdout and exit.")
	execOnly    = flag.String("exec_only", "", "If set, execute this reflection program.")
	buildFlags  = flag.String("build_flags", "", "Additional flags for go build.")
	useExtTypes = flag.Bool("use_ext_types", false, "Don't use 'interface{}' for types not in this package or the standard library.")
)

func writeProgram(importPath string, types []string, values []string) ([]byte, error) {
	var program bytes.Buffer
	data := reflectData{
		ImportPath:  importPath,
		UseExtTypes: *useExtTypes,
		Types:       types,
		Values:      values,
	}
	if err := reflectProgram.Execute(&program, &data); err != nil {
		return nil, err
	}
	return program.Bytes(), nil
}

// run the given program and parse the output as a model.Package.
func run(program string) (*model.PackedPkg, error) {
	f, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}

	filename := f.Name()
	defer os.Remove(filename)
	if err := f.Close(); err != nil {
		return nil, err
	}

	// Run the program.
	cmd := exec.Command(program, "-output", filename)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	f, err = os.Open(filename)
	if err != nil {
		return nil, err
	}

	// Process output.
	var pkg model.PackedPkg
	if err := gob.NewDecoder(f).Decode(&pkg); err != nil {
		return nil, err
	}

	if err := f.Close(); err != nil {
		return nil, err
	}

	return &pkg, nil
}

// runInDir writes the given program into the given dir, runs it there, and
// parses the output as a model.Package.
func runInDir(program []byte, dir string) (*model.PackedPkg, error) {
	// We use TempDir instead of TempFile so we can control the filename.
	tmpDir, err := ioutil.TempDir(dir, "depstubber_reflect_")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			log.Printf("failed to remove temp directory: %s", err)
		}
	}()
	const progSource = "prog.go"
	var progBinary = "prog.bin"
	if runtime.GOOS == "windows" {
		// Windows won't execute a program unless it has a ".exe" suffix.
		progBinary += ".exe"
	}

	if err := ioutil.WriteFile(filepath.Join(tmpDir, progSource), program, 0600); err != nil {
		return nil, err
	}

	{
		// Copy go.mod into the build directory:
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Unable to load current directory: %v", err)
		}

		modRoot := findModuleRoot(wd)

		if modRoot != "" {
			MustCopyFile(filepath.Join(modRoot, "go.mod"), filepath.Join(tmpDir, "go.mod"))
		}
	}

	cmdArgs := []string{"build", "-mod=mod"}
	if *buildFlags != "" {
		cmdArgs = append(cmdArgs, strings.Split(*buildFlags, " ")...)
	}
	cmdArgs = append(cmdArgs, "-o", progBinary, progSource)

	// Build the program.
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = tmpDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return run(filepath.Join(tmpDir, progBinary))
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

var exportedIdRegex = regexp.MustCompile(`(\p{Lu}(\pL|\pN)*)(\.\p{Lu}(\pL|\pN))*`)

func exportedId(id string) bool {
	return exportedIdRegex.MatchString(id)
}

// reflectMode generates mocks via reflection on an interface.
func reflectMode(importPath string, types []string, values []string) (*model.PackedPkg, error) {
	for _, t := range types {
		if !exportedId(t) {
			return nil, fmt.Errorf("%s is not a valid exported name.", t)
		}
	}

	for _, v := range values {
		if !exportedId(v) {
			return nil, fmt.Errorf("%s is not a valid exported name.", v)
		}
	}

	if *execOnly != "" {
		return run(*execOnly)
	}

	program, err := writeProgram(importPath, types, values)
	if err != nil {
		return nil, err
	}

	if *progOnly {
		if _, err := os.Stdout.Write(program); err != nil {
			return nil, err
		}
		os.Exit(0)
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Unable to load current directory: %v", err)
	}

	// Try to run the reflection program  in the current working directory.
	if p, err := runInDir(program, wd); err == nil {
		return p, nil
	}

	// Try to run the program in the same directory as the input package.
	if p, err := build.Import(importPath, wd, build.FindOnly); err == nil {
		dir := p.Dir
		if p, err := runInDir(program, dir); err == nil {
			return p, nil
		}
	}

	// Try to run it in a standard temp directory.
	return runInDir(program, "")
}

type reflectData struct {
	ImportPath  string
	UseExtTypes bool
	Types       []string
	Values      []string
}

// This program reflects on an interface value, and prints the
// gob encoding of a model.Package to standard output.
// JSON doesn't work because of the model.Type interface.
var reflectProgram = template.Must(template.New("program").Parse(`
package main

import (
	"encoding/gob"
	"flag"
	"fmt"
	"os"
	"reflect"

	"github.com/github/depstubber/model"

	pkg_ {{printf "%q" .ImportPath}}
)

var output = flag.String("output", "", "The output file name, or empty to use stdout.")

func main() {
	flag.Parse()

	types := []struct{
		sym string
		typ reflect.Type
	}{
		{{range .Types}}
		{ {{printf "%q" .}}, reflect.TypeOf((*pkg_.{{.}})(nil)).Elem() },
		{{end}}
	}

	values := []struct{
		sym string
		val reflect.Value
	}{
		{{range .Values}}
		{ {{printf "%q" .}}, reflect.ValueOf(pkg_.{{.}}) },
		{{end}}
	}

	// NOTE: This behaves contrary to documented behaviour if the
	// package name is not the final component of the import path.
	// The reflect package doesn't expose the package name, though.
	pkg := model.NewPackage({{printf "%q" .ImportPath}}, {{.UseExtTypes}})

	for _, t := range types {
		err := pkg.AddType(t.sym, t.typ)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Reflection: %v\n", err)
			os.Exit(1)
		}
	}

	for _, v := range values {
		err := pkg.AddValue(v.sym, v.val)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Reflection: %v\n", err)
			os.Exit(1)
		}
	}

	outfile := os.Stdout
	if len(*output) != 0 {
		var err error
		outfile, err = os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open output file %q", *output)
		}
		defer func() {
			if err := outfile.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to close output file %q", *output)
				os.Exit(1)
			}
		}()
	}

	if err := gob.NewEncoder(outfile).Encode(model.PackPkg(pkg)); err != nil {
		fmt.Fprintf(os.Stderr, "gob encode: %v\n", err)
		os.Exit(1)
	}
}
`))
