package models

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// PluginSettings is the user-facing datasource configuration.
//
//   - Endpoint: the base URL of the Echo HTTP API, e.g. "https://echo.internal:8081".
//     Must NOT include "/api/v2"; the backend appends paths.
//   - InsecureSkipVerify: if true, the backend skips TLS certificate
//     verification on outbound requests. Intended only for local /
//     self-signed development; production datasources must leave it
//     false. Wired through to the http.Transport's tls.Config.
type PluginSettings struct {
	Endpoint           string                `json:"endpoint"`
	InsecureSkipVerify bool                  `json:"insecureSkipVerify"`
	Secrets            *SecretPluginSettings `json:"-"`
}

// SecretPluginSettings holds fields that live in Grafana's secure
// store. The bearer field is what gets injected into the
// Authorization header on every outbound request.
type SecretPluginSettings struct {
	Bearer string `json:"bearer"`
}

func LoadPluginSettings(source backend.DataSourceInstanceSettings) (*PluginSettings, error) {
	settings := PluginSettings{}
	if len(source.JSONData) > 0 {
		if err := json.Unmarshal(source.JSONData, &settings); err != nil {
			return nil, fmt.Errorf("could not unmarshal PluginSettings json: %w", err)
		}
	}
	settings.Secrets = loadSecretPluginSettings(source.DecryptedSecureJSONData)
	return &settings, nil
}

func loadSecretPluginSettings(source map[string]string) *SecretPluginSettings {
	return &SecretPluginSettings{
		Bearer: source["bearer"],
	}
}
