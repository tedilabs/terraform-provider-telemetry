package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	log.SetFlags(0)
	if err := verifyRelease(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("PASS: six platform ZIPs, binary names, license, protocol 6 manifest, SHA256 checksums")
}

func verifyRelease() error {
	data, err := os.ReadFile("dist/metadata.json")
	if err != nil {
		return err
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return err
	}
	if metadata.Version == "" {
		return fmt.Errorf("release metadata has no version")
	}
	prefix := "terraform-provider-telemetry_" + metadata.Version
	expected := make(map[string]string)
	for _, platform := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			name := fmt.Sprintf("%s_%s_%s.zip", prefix, platform, arch)
			binary := "terraform-provider-telemetry_v" + metadata.Version
			if platform == "windows" {
				binary += ".exe"
			}
			if err := verifyArchive(filepath.Join("dist", name), binary); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			expected[name] = filepath.Join("dist", name)
		}
	}
	archives, err := filepath.Glob("dist/*.zip")
	if err != nil {
		return err
	}
	if len(archives) != len(expected) {
		return fmt.Errorf("unexpected platform archives: %v", archives)
	}

	// GoReleaser uses the source manifest in snapshot mode; publishing renames it.
	expected[prefix+"_manifest.json"] = "terraform-registry-manifest.json"
	data, err = os.ReadFile(filepath.Join("dist", prefix+"_SHA256SUMS"))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return fmt.Errorf("invalid checksum entry: %q", line)
		}
		path, ok := expected[fields[1]]
		if !ok {
			return fmt.Errorf("unexpected or duplicate checksum entry: %s", fields[1])
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(contents)) != fields[0] {
			return fmt.Errorf("checksum mismatch: %s", fields[1])
		}
		delete(expected, fields[1])
	}
	if len(expected) != 0 {
		return fmt.Errorf("missing checksum entries: %v", expected)
	}

	data, err = os.ReadFile("terraform-registry-manifest.json")
	if err != nil {
		return err
	}
	var manifest struct {
		Version  int `json:"version"`
		Metadata struct {
			ProtocolVersions []string `json:"protocol_versions"`
		} `json:"metadata"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("unexpected data after registry manifest")
	}
	if manifest.Version != 1 || len(manifest.Metadata.ProtocolVersions) != 1 || manifest.Metadata.ProtocolVersions[0] != "6.0" {
		return fmt.Errorf("expected a version 1 manifest with provider protocol 6.0")
	}
	return nil
}

func verifyArchive(path, binary string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer archive.Close()
	expected := map[string]bool{"LICENSE": true, binary: true}
	for _, file := range archive.File {
		if !expected[file.Name] {
			return fmt.Errorf("unexpected or duplicate archive entry: %s", file.Name)
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, reader)
		reader.Close()
		if err != nil {
			return err
		}
		delete(expected, file.Name)
	}
	if len(expected) != 0 {
		return fmt.Errorf("missing archive entries: %v", expected)
	}
	return nil
}
