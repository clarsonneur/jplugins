package main

import (
	"fmt"
	"regexp"

	goversion "github.com/hashicorp/go-version"
)

type versionStruct struct {
	original string
	version  *goversion.Version
}

func (v versionStruct) String() string {
	return v.original
}

func (v *versionStruct) Set(value string) (err error) {
	if v == nil {
		return
	}
	v.original = value

	if value != "new" {

		// Accept version with extra string suffixed. But keep just valid version string for goversion.
		versionRegexp := regexp.MustCompile(`^([0-9][0-9A-Za-z\-~.]*)([^0-9A-Za-z\-~.].*)?$`)
		matches := versionRegexp.FindStringSubmatch(value)
		if matches == nil {
			return fmt.Errorf("Undetected version string in '%s'", value)
		}
		v.version, err = goversion.NewVersion(matches[1])
	} else {
		v.version = nil
	}
	return
}

func (v versionStruct) Get() (ret *goversion.Version) {
	return v.version
}
