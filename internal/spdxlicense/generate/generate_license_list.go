package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/scylladb/go-set/strset"
)

// This program generates license_list.go.
const (
	source = "license_list.go"
	url    = "https://spdx.org/licenses/licenses.json"
)

var tmp = template.Must(template.New("").Parse(`// Code generated by go generate; DO NOT EDIT.
// This file was generated by robots at {{ .Timestamp }}
// using data from {{ .URL }}
package spdxlicense

const Version = {{ printf "%q" .Version }}

var licenseIDs = map[string]string{
{{- range $k, $v := .LicenseIDs }}
	{{ printf "%q" $k }}: {{ printf "%q" $v }},
{{- end }}
}
`))

var versionMatch = regexp.MustCompile(`-([0-9]+)\.?([0-9]+)?\.?([0-9]+)?\.?`)

type LicenseList struct {
	Version  string `json:"licenseListVersion"`
	Licenses []struct {
		ID          string   `json:"licenseId"`
		Name        string   `json:"name"`
		Text        string   `json:"licenseText"`
		Deprecated  bool     `json:"isDeprecatedLicenseId"`
		OSIApproved bool     `json:"isOsiApproved"`
		SeeAlso     []string `json:"seeAlso"`
	} `json:"licenses"`
}

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("unable to get licenses list: %+v", err)
	}

	var result LicenseList
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("unable to decode license list: %+v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Fatalf("unable to close body: %+v", err)
		}
	}()

	f, err := os.Create(source)
	if err != nil {
		return fmt.Errorf("unable to create %q: %+v", source, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Fatalf("unable to close %q: %+v", source, err)
		}
	}()

	licenseIDs := processSPDXLicense(result)

	err = tmp.Execute(f, struct {
		Timestamp  time.Time
		URL        string
		Version    string
		LicenseIDs map[string]string
	}{
		Timestamp:  time.Now(),
		URL:        url,
		Version:    result.Version,
		LicenseIDs: licenseIDs,
	})

	if err != nil {
		return fmt.Errorf("unable to generate template: %+v", err)
	}
	return nil
}

// Parsing the provided SPDX license list necessitates a two pass approach.
// The first pass is only related to what SPDX considers the truth. These K:V pairs will never be overwritten.
// The second pass attempts to generate known short/long version listings for each key.
// For info on some short name conventions see this document:
// https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/#license-short-name.
// The short long listing generation attempts to build all license permutations for a given key.
// The new keys are then also associated with their relative SPDX value. If a key has already been entered
// we know to ignore it since it came from the first pass which is considered the SPDX source of truth.
// We also sort the licenses for the second pass so that cases like `GPL-1` associate to `GPL-1.0` and not `GPL-1.1`.
func processSPDXLicense(result LicenseList) map[string]string {
	// first pass build map
	var licenseIDs = make(map[string]string)
	for _, l := range result.Licenses {
		cleanID := strings.ToLower(l.ID)
		if _, exists := licenseIDs[cleanID]; exists {
			log.Fatalf("duplicate license ID found: %q", cleanID)
		}
		licenseIDs[cleanID] = l.ID
	}

	sort.Slice(result.Licenses, func(i, j int) bool {
		return result.Licenses[i].ID < result.Licenses[j].ID
	})

	// second pass build exceptions
	// do not overwrite if already exists
	for _, l := range result.Licenses {
		var multipleID []string
		cleanID := strings.ToLower(l.ID)
		multipleID = append(multipleID, buildLicensePermutations(cleanID)...)
		for _, id := range multipleID {
			if _, exists := licenseIDs[id]; !exists {
				licenseIDs[id] = l.ID
			}
		}
	}

	return licenseIDs
}

func buildLicensePermutations(license string) (perms []string) {
	lv := findLicenseVersion(license)
	vp := versionPermutations(lv)

	version := strings.Join(lv, ".")
	for _, p := range vp {
		perms = append(perms, strings.Replace(license, version, p, 1))
	}

	return perms
}

func findLicenseVersion(license string) (version []string) {
	versionList := versionMatch.FindAllStringSubmatch(license, -1)

	if len(versionList) == 0 {
		return version
	}

	for i, v := range versionList[0] {
		if v != "" && i != 0 {
			version = append(version, v)
		}
	}

	return version
}

func versionPermutations(version []string) []string {
	ver := append([]string(nil), version...)
	perms := strset.New()
	for i := 1; i <= 3; i++ {
		if len(ver) < i+1 {
			ver = append(ver, "0")
		}

		perm := strings.Join(ver[:i], ".")
		badCount := strings.Count(perm, "0") + strings.Count(perm, ".")

		if badCount != len(perm) {
			perms.Add(perm)
		}
	}

	return perms.List()
}
