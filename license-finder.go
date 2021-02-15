package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-enry/go-license-detector/v4/licensedb"
	"github.com/go-enry/go-license-detector/v4/licensedb/filer"
)

// copyLicenses finds license files in the provided directories,
// and copies them into the vendor directories of the stubbed packages.
func copyLicenses(licenseDirs []string) error {
	if licenseDirs == nil {
		return nil
	}
	for _, licenseSearchDir := range licenseDirs {
		fl, err := filer.FromDirectory(licenseSearchDir)
		if err != nil {
			return err
		}
		licenses, err := licensedb.Detect(fl)
		if err != nil {
			return err
		}
		filenames := make([]string, 0)
		{
			for _, match := range licenses {
				for fName := range match.Files {
					filenames = append(filenames, fName)
				}
			}
		}

		for _, licenseRelativePath := range filenames {
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
	return nil
}
