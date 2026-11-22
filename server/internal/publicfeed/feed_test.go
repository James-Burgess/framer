package publicfeed

import (
	"encoding/json"
	"testing"
)

func TestFlexStringUnmarshal(t *testing.T) {
	raw := `{
		"query": {
			"pages": {
				"123": {
					"pageid": 123,
					"title": "File:Test.jpg",
					"imageinfo": [{
						"extmetadata": {
							"ObjectName": {"value": "Sunset over Table Mountain"},
							"DateTimeOriginal": {"value": 2024},
							"GPSLatitude": {"value": -33.9249},
							"Restricted": {"value": false},
							"Empty": {"value": null}
						}
					}]
				}
			}
		}
	}`

	var resp struct {
		Query struct {
			Pages map[string]struct {
				ImageInfo []struct {
					ExtMetadata map[string]struct {
						Value flexString `json:"value"`
					} `json:"extmetadata"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}

	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("failed to unmarshal mixed extmetadata: %v", err)
	}
	meta := resp.Query.Pages["123"].ImageInfo[0].ExtMetadata
	if got := string(meta["ObjectName"].Value); got != "Sunset over Table Mountain" {
		t.Fatalf("ObjectName = %q", got)
	}
	if got := string(meta["DateTimeOriginal"].Value); got != "2024" {
		t.Fatalf("DateTimeOriginal = %q", got)
	}
	if got := string(meta["GPSLatitude"].Value); got != "-33.9249" {
		t.Fatalf("GPSLatitude = %q", got)
	}
	if got := string(meta["Restricted"].Value); got != "false" {
		t.Fatalf("Restricted = %q", got)
	}
	if got := string(meta["Empty"].Value); got != "" {
		t.Fatalf("Empty = %q", got)
	}
}
