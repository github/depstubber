package main

import (
	"bytes"
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/golang/dep/gps/paths"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/vcs"
)

type CombinedErrors struct {
	errs []error
}

func (ce *CombinedErrors) Error() string {
	buf := new(bytes.Buffer)
	buf.WriteString("The following errors occurred:")
	for _, err := range ce.errs {
		if err != nil {
			buf.WriteString("\n - " + err.Error())
		}
	}
	return buf.String()
}

func allNil(errs ...error) bool {
	for _, err := range errs {
		if err != nil {
			return false
		}
	}
	return true
}

func CombineErrors(errs ...error) error {
	if len(errs) == 0 || allNil(errs...) {
		return nil
	}
	return &CombinedErrors{
		errs: errs,
	}
}

func loadPackage(startPkg string, dir string) (*packages.Package, error) {
	config := &packages.Config{
		Mode: packages.LoadSyntax | packages.NeedModule,
	}

	// Set the package loader Dir to the `dir`; that will force
	// the package loader to use the `go.mod` file and thus
	// load the wanted version of the package:
	config.Dir = dir

	pkgs, err := packages.Load(config, startPkg)
	if err != nil {
		return nil, fmt.Errorf("error while running packages.Load: %s", err)
	}

	var errs []error
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, err := range pkg.Errors {
			errs = append(errs, err)
		}
	})
	if len(errs) > 0 {
		return nil, fmt.Errorf("error while packages.Load: %s", CombineErrors(errs...))
	}

	return pkgs[0], nil
}

// DeduplicateStrings returns a new slice with duplicate values removed.
func DeduplicateStrings(slice []string) []string {
	if len(slice) <= 1 {
		return slice
	}

	result := []string{}
	seen := make(map[string]struct{})
	for _, val := range slice {
		if _, ok := seen[val]; !ok {
			result = append(result, val)
			seen[val] = struct{}{}
		}
	}
	return result
}

// removeBlankIdentifier returns a new slice with blank identifier `_` removed.
func removeBlankIdentifier(slice []string) []string {
	result := []string{}
	for _, val := range slice {
		if val != "_" {
			result = append(result, val)
		}
	}
	return result
}

// removeUnexported returns a new slice with all unexported identifiers removed.
func removeUnexported(slice []string) []string {
	result := []string{}
	for _, val := range slice {
		if token.IsExported(val) {
			result = append(result, val)
		}
	}
	return result
}

func autoDetect(startPkg string, dir string) (map[string][]string, map[string][]string, error) {
	pk, err := loadPackage(startPkg, dir)
	if err != nil {
		return nil, nil, fmt.Errorf("error while loading package: %s", err)
	}

	rootOfStartPkg, _ := vcs.RepoRootForImportPath(pk.Types.Path(), false)

	pathToTypeNames := make(map[string][]string)
	pathToFuncAndVarNames := make(map[string][]string)

	for _, obj := range pk.TypesInfo.Uses {
		if obj.Pkg() == nil || obj.Pkg().Path() == "" {
			// Skip objects that don't belong to a package.
			continue
		}

		if isStd := paths.IsStandardImportPath(obj.Pkg().Path()); isStd {
			// Skip objects that belong to a Go standard library (supposedly).
			continue
		}

		if packageIsSamePath := obj.Pkg().Path() == pk.Types.Path(); packageIsSamePath {
			// Skip objects that belong to the initial package that was scanned.
			continue
		}

		if notExported := !obj.Exported(); notExported {
			panic(fmt.Sprintf("Encountered unexpected unexported type %v, which should not be accessible by this package (%s).", obj, obj.Pkg().Path()))
		}

		// Check whether obj.Pkg().Path() is a subpath of pk.Types.Path() (or the other way round), i.e. they belong to the same root package.
		// Skip objects belonging to packages that have the same root as the initial package.
		pathsOverlap := strings.HasPrefix(obj.Pkg().Path(), pk.Types.Path()+"/") || strings.HasPrefix(pk.Types.Path(), obj.Pkg().Path()+"/")
		if rootOfStartPkg != nil {
			// Check with root:
			rootOfThisObjPkg, err := vcs.RepoRootForImportPath(obj.Pkg().Path(), false)
			if err == nil && rootOfStartPkg.Root == rootOfThisObjPkg.Root {
				continue
			} else {
				// Check with string prefix:
				if pathsOverlap {
					continue
				}
			}
		} else {
			// Check with string prefix:
			if pathsOverlap {
				continue
			}
		}

		switch thing := obj.(type) {
		case *types.TypeName:
			{
				switch namedOrSignature := obj.Type().(type) {
				case *types.Named:
					{
						pkgPath := namedOrSignature.Obj().Pkg().Path()
						pathToTypeNames[pkgPath] = append(pathToTypeNames[pkgPath], namedOrSignature.Obj().Name())
					}
				default:
					fmt.Printf("ignoring type %T for object %s", obj.Type(), obj.String())
					//panic(fmt.Sprintf("unknown type %T for object %s", obj.Type(), obj.String()))
				}
			}
		case *types.Const:
			{
				pkgPath := thing.Pkg().Path()
				pathToFuncAndVarNames[pkgPath] = append(pathToFuncAndVarNames[pkgPath], thing.Name())
			}
		case *types.Var:
			{
				// Ignore fields
				isNotAField := !thing.IsField()
				if isNotAField {
					pkgPath := thing.Pkg().Path()
					pathToFuncAndVarNames[pkgPath] = append(pathToFuncAndVarNames[pkgPath], thing.Name())
				}
			}
		case *types.Func:
			switch sig := thing.Type().(type) {
			case *types.Signature:
				{
					if sig.Recv() != nil {
						// This is a method.
						// Add receiver:
						pkgPath := sig.Recv().Pkg().Path()
						pathToFuncAndVarNames[pkgPath] = append(pathToFuncAndVarNames[pkgPath], sig.Recv().Name())
					} else {
						// This is a normal function.
						pkgPath := thing.Pkg().Path()
						pathToFuncAndVarNames[pkgPath] = append(pathToFuncAndVarNames[pkgPath], thing.Name())
					}
				}
			default:
				panic(fmt.Sprintf("non-signature type %T for function %s", thing.Type(), obj.String()))
			}
		default:
			panic(fmt.Sprintf("unknown type %T for object %s", obj, obj.String()))
		}
	}

	{
		// Deduplicate and sort:
		for pkgPath := range pathToTypeNames {
			dedup := DeduplicateStrings(pathToTypeNames[pkgPath])
			dedup = removeBlankIdentifier(dedup)
			dedup = removeUnexported(dedup)
			sort.Strings(dedup)
			pathToTypeNames[pkgPath] = dedup
		}
		for pkgPath := range pathToFuncAndVarNames {
			dedup := DeduplicateStrings(pathToFuncAndVarNames[pkgPath])
			dedup = removeBlankIdentifier(dedup)
			dedup = removeUnexported(dedup)
			sort.Strings(dedup)
			pathToFuncAndVarNames[pkgPath] = dedup
		}
	}

	return pathToTypeNames, pathToFuncAndVarNames, nil
}

// FormatDepstubberComment returns the `depstubber` comment that will be used to stub types.
// The returned string is prefixed with //
func FormatDepstubberComment(path string, typeNames []string, funcAndVarNames []string) string {
	var first string
	if len(typeNames) > 0 {
		typeNames = DeduplicateStrings(typeNames)
		sort.Strings(typeNames)
		first = strings.Join(typeNames, ",")
	} else {
		first = `""`
	}

	var second string
	if len(funcAndVarNames) > 0 {
		funcAndVarNames = DeduplicateStrings(funcAndVarNames)
		sort.Strings(funcAndVarNames)
		second = strings.Join(funcAndVarNames, ",")
	}

	return strings.TrimSpace(fmt.Sprintf(
		"//go:generate depstubber -vendor %s %s %s",
		path,
		first,
		second,
	))
}

// printGoGenerateComments prints the `go:generate` depstubber comments.
func printGoGenerateComments(pathToTypeNames map[string][]string, pathToFuncAndVarNames map[string][]string) {
	pkgPaths := make([]string, 0)
	{
		// Get a list of all package paths:
		for path := range pathToTypeNames {
			pkgPaths = append(pkgPaths, path)
		}
		for path := range pathToFuncAndVarNames {
			pkgPaths = append(pkgPaths, path)
		}
		pkgPaths = DeduplicateStrings(pkgPaths)
		sort.Strings(pkgPaths)
	}

	for _, pkgPath := range pkgPaths {
		comment := FormatDepstubberComment(
			pkgPath,
			pathToTypeNames[pkgPath],
			pathToFuncAndVarNames[pkgPath],
		)
		fmt.Println(comment)
	}
}
