package main

import (
	"bytes"
	"fmt"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type CombinedErrors struct {
	errs []error
}

func (ce *CombinedErrors) Error() string {
	buf := new(bytes.Buffer)
	buf.WriteString("The following errors occurred:")
	for _, err := range ce.errs {
		if err != nil {
			buf.WriteString("\n -  " + err.Error())
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
		return nil, fmt.Errorf("error while packages.Load: %s", err)
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

func IsStandardImportPath(path string) bool {
	i := strings.Index(path, "/")
	if i < 0 {
		i = len(path)
	}
	elem := path[:i]
	return !strings.Contains(elem, ".")
}

func autoDetect(startPkg string, dir string) (map[string][]string, map[string][]string, error) {
	pk, err := loadPackage(startPkg, dir)
	if err != nil {
		return nil, nil, fmt.Errorf("error while loading package: %s", err)
	}

	pathToTypeNames := make(map[string][]string)
	pathToFuncAndVarNames := make(map[string][]string)

	for _, obj := range pk.TypesInfo.Uses {
		if obj.Pkg() == nil || obj.Pkg().Path() == "" {
			// Skip objects that don't belong to a package.
			continue
		}

		if isStd := IsStandardImportPath(obj.Pkg().Path()); isStd {
			// Skip objects that belong to a Go standard library.
			continue
		}

		packageIsSame := obj.Pkg() == pk.Types
		packageIsSamePath := obj.Pkg().Path() == pk.Types.Path()

		if packageIsSamePath || packageIsSame {
			// Skip objects that belong to the initial package that was scanned.
			continue
		}

		if notExported := !obj.Exported(); notExported {
			// Skip unexported objects.
			// p.Package.TypesInfo.Uses also contains used objects that
			// are declared inside the same package, and this means
			// that some objects might not be exported.
			panic("This should not happen at this point.")
			continue
		}

		switch thing := obj.(type) {
		case *types.TypeName:
			{
				if thing.IsAlias() {
					// TODO: does this change something?
				}

				switch namedOrSignature := obj.Type().(type) {
				case *types.Named:
					{
						pkgPath := namedOrSignature.Obj().Pkg().Path()
						pathToTypeNames[pkgPath] = append(pathToTypeNames[pkgPath], namedOrSignature.Obj().Name())
					}
				default:
					panic(fmt.Sprintf("unknown type %T", obj.Type()))
				}

			}
		case *types.Const:
			{
				pkgPath := thing.Pkg().Path()
				pathToFuncAndVarNames[pkgPath] = append(pathToFuncAndVarNames[pkgPath], thing.Name())
			}
		case *types.Var:
			{
				pkgPath := thing.Pkg().Path()
				pathToFuncAndVarNames[pkgPath] = append(pathToFuncAndVarNames[pkgPath], thing.Name())
			}
		case *types.Func:

			switch thing.Type().(type) {
			case *types.Signature:
				{
					pkgPath := thing.Pkg().Path()
					pathToFuncAndVarNames[pkgPath] = append(pathToFuncAndVarNames[pkgPath], thing.Name())
				}
			default:
				panic(fmt.Sprintf("unknown type %T", thing.Type()))
			}
		default:
			panic(fmt.Sprintf("unknown type %T", obj))
		}
	}

	{
		// Deduplicate and sort:
		for pkgPath := range pathToTypeNames {
			dedup := DeduplicateStrings(pathToTypeNames[pkgPath])
			sort.Strings(dedup)
			pathToTypeNames[pkgPath] = dedup
		}
		for pkgPath := range pathToFuncAndVarNames {
			dedup := DeduplicateStrings(pathToFuncAndVarNames[pkgPath])
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
	} else {
		second = `""`
	}

	return fmt.Sprintf(
		"//go:generate depstubber -vendor %s %s %s",
		path,
		first,
		second,
	)
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

	// If `depstubber -write_module_txt` is not executed, then
	// you'll encunter a `go: inconsistent vendoring in ...` error;
	// Printing this as a reminder can save time for a few people.
	fmt.Println("//go:generate depstubber -write_module_txt")
}
