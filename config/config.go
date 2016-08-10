// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"bytes"
	"errors"

	"github.com/coreos/ignition/config/types"
	"github.com/coreos/ignition/config/v1"
	"github.com/coreos/ignition/config/validate"
	"github.com/coreos/ignition/config/validate/report"

	json "github.com/ajeddeloh/go-json"
	"go4.org/errorutil"
)

var (
	ErrCloudConfig = errors.New("not a config (found coreos-cloudconfig)")
	ErrEmpty       = errors.New("not a config (empty)")
	ErrScript      = errors.New("not a config (found coreos-cloudinit script)")
	ErrDeprecated  = errors.New("config format deprecated")
	ErrInvalid     = errors.New("config is not valid")
)

// Parse parses the raw config into a types.Config struct and generates a report of any
// errors, warnings, info, and deprecations it encountered
func Parse(rawConfig []byte) (types.Config, report.Report, error) {
	switch version(rawConfig) {
	case types.IgnitionVersion{Major: 1}:
		config, err := ParseFromV1(rawConfig)
		if err != nil {
			return types.Config{}, report.ReportFromError(err, report.EntryError), err
		}

		return config, report.ReportFromError(ErrDeprecated, report.EntryDeprecated), nil
	default:
		return ParseFromLatest(rawConfig)
	}
}

func ParseFromLatest(rawConfig []byte) (types.Config, report.Report, error) {
	if isEmpty(rawConfig) {
		return types.Config{}, report.Report{}, ErrEmpty
	} else if isCloudConfig(rawConfig) {
		return types.Config{}, report.Report{}, ErrCloudConfig
	} else if isScript(rawConfig) {
		return types.Config{}, report.Report{}, ErrScript
	}

	var err error
	var config types.Config

	// These errors are fatal and the config should not be further validated
	if err = json.Unmarshal(rawConfig, &config); err == nil {
		err = config.Ignition.Version.AssertValid()
	}

	// Handle json syntax and type errors first, since they are fatal but have offset info
	if serr, ok := err.(*json.SyntaxError); ok {
		line, col, highlight := errorutil.HighlightBytePosition(bytes.NewReader(rawConfig), serr.Offset)
		return types.Config{},
			report.Report{
				Entries: []report.Entry{{
					Kind:      report.EntryError,
					Message:   serr.Error(),
					Line:      line,
					Column:    col,
					Highlight: highlight,
				}},
			},
			ErrInvalid
	}

	if terr, ok := err.(*json.UnmarshalTypeError); ok {
		line, col, highlight := errorutil.HighlightBytePosition(bytes.NewReader(rawConfig), terr.Offset)
		return types.Config{},
			report.Report{
				Entries: []report.Entry{{
					Kind:      report.EntryError,
					Message:   terr.Error(),
					Line:      line,
					Column:    col,
					Highlight: highlight,
				}},
			},
			ErrInvalid
	}

	// Handle other fatal errors (i.e. invalid version)
	if err != nil {
		return types.Config{}, report.ReportFromError(err, report.EntryError), err
	}

	// Unmarshal again to a json.Node to get offset information for building a report
	var ast json.Node
	var r report.Report
	if err := json.Unmarshal(rawConfig, &ast); err != nil {
		r.Add(report.Entry{
			Kind:    report.EntryWarning,
			Message: "Ignition could not unmarshal your config for reporting line numbers. This should never happen. Please file a bug.",
		})
		r.Merge(validate.ValidateWithoutSource(config))
	} else {
		r.Merge(validate.Validate(config, ast, bytes.NewReader(rawConfig)))
	}

	if r.IsFatal() {
		return types.Config{}, r, ErrInvalid
	}

	return config, r, nil
}

func ParseFromV1(rawConfig []byte) (types.Config, error) {
	config, err := v1.Parse(rawConfig)
	if err != nil {
		return types.Config{}, err
	}

	return TranslateFromV1(config)
}

func version(rawConfig []byte) types.IgnitionVersion {
	var composite struct {
		Version  *int `json:"ignitionVersion"`
		Ignition struct {
			Version *types.IgnitionVersion `json:"version"`
		} `json:"ignition"`
	}

	if json.Unmarshal(rawConfig, &composite) == nil {
		if composite.Ignition.Version != nil {
			return *composite.Ignition.Version
		} else if composite.Version != nil {
			return types.IgnitionVersion{Major: int64(*composite.Version)}
		}
	}

	return types.IgnitionVersion{}
}

func isEmpty(userdata []byte) bool {
	return len(userdata) == 0
}
