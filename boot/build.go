/*
 * Copyright 2018-2020 the original author or authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package boot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildpacks/libcnb"
	"github.com/paketo-buildpacks/libjvm"
	"github.com/paketo-buildpacks/libpak/bard"
	"gopkg.in/yaml.v2"
)

type Build struct {
	Logger bard.Logger
}

func (b Build) Build(context libcnb.BuildContext) (libcnb.BuildResult, error) {
	manifest, err := libjvm.NewManifest(context.Application.Path)
	if err != nil {
		return libcnb.BuildResult{}, fmt.Errorf("unable to read manifest in %s\n%w", context.Application.Path, err)
	}

	version, ok := manifest.Get("Spring-Boot-Version")
	if !ok {
		return libcnb.BuildResult{}, nil
	}

	b.Logger.Title(context.Buildpack)
	result := libcnb.NewBuildResult()

	classes, ok := manifest.Get("Spring-Boot-Classes")
	if !ok {
		return libcnb.BuildResult{}, fmt.Errorf("manifest does not contain Spring-Boot-Classes")
	}
	lib, ok := manifest.Get("Spring-Boot-Lib")
	if !ok {
		return libcnb.BuildResult{}, fmt.Errorf("manifest does not container Spring-Boot-Lib")
	}

	result.Labels = append(result.Labels, libcnb.Label{Key: "org.springframework.boot.version", Value: version})

	c, err := NewConfigurationMetadataFromPath(context.Application.Path)
	if err != nil {
		return libcnb.BuildResult{}, fmt.Errorf("unable to read configuration metadata from %s\n%w", classes, err)
	}

	file := filepath.Join(lib, "*.jar")
	files, err := filepath.Glob(file)
	if err != nil {
		return libcnb.BuildResult{}, fmt.Errorf("unable to glob %s\n%w", file, err)
	}

	for _, file := range files {
		d, err := NewConfigurationMetadataFromJAR(file)
		if err != nil {
			return libcnb.BuildResult{}, fmt.Errorf("unable to read configuration metadata from %s\n%w", file, err)
		}

		c.Groups = append(c.Groups, d.Groups...)
		c.Properties = append(c.Properties, d.Properties...)
		c.Hints = append(c.Hints, d.Hints...)
	}

	if len(c.Groups) > 0 || len(c.Properties) > 0 || len(c.Hints) > 0 {
		b := &bytes.Buffer{}
		if err := json.NewEncoder(b).Encode(c); err != nil {
			return libcnb.BuildResult{}, fmt.Errorf("unable to encode configuration metadata\n%w", err)
		}

		result.Labels = append(result.Labels, libcnb.Label{
			Key:   "org.springframework.boot.spring-configuration-metadata.json",
			Value: strings.TrimSpace(b.String()),
		})
	}

	if s, ok := manifest.Get("Implementation-Title"); ok {
		result.Labels = append(result.Labels, libcnb.Label{Key: "org.opencontainers.image.title", Value: s})
	}

	if s, ok := manifest.Get("Implementation-Version"); ok {
		result.Labels = append(result.Labels, libcnb.Label{Key: "org.opencontainers.image.version", Value: s})
	}

	d, err := libjvm.NewMavenJARListing(filepath.Join(context.Application.Path, lib))
	if err != nil {
		return libcnb.BuildResult{}, fmt.Errorf("unable to generate dependencies from %s\n%w", context.Application.Path, err)
	}
	result.Plan.Entries = append(result.Plan.Entries, libcnb.BuildpackPlanEntry{
		Name:     "dependencies",
		Metadata: map[string]interface{}{"dependencies": d},
	})

	if index, ok := manifest.Get("Spring-Boot-Layers-Index"); ok {
		b.Logger.Header("Creating slices from layers index")

		file := filepath.Join(context.Application.Path, index)
		in, err := os.Open(file)
		if err != nil {
			return libcnb.BuildResult{}, fmt.Errorf("unable to open %s\n%w", file, err)
		}
		defer in.Close()

		var layers []map[string][]string
		if err := yaml.NewDecoder(in).Decode(&layers); err != nil {
			return libcnb.BuildResult{}, fmt.Errorf("unable to decode %s\n%w", file, err)
		}

		for _, layer := range layers {
			for name, paths := range layer {
				b.Logger.Body(name)
				result.Slices = append(result.Slices, libcnb.Slice{Paths: paths})
			}
		}
	}

	return result, nil
}
