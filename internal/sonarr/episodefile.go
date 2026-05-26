package sonarr

import (
	"encoding/json"
	"fmt"
)

// EpisodeFilePath returns the on-disk file path of an episode (for playback).
func (c *Client) EpisodeFilePath(episodeID int) (string, error) {
	b, err := c.apiGet(fmt.Sprintf("/api/v3/episode/%d", episodeID))
	if err != nil {
		return "", err
	}
	var ep struct {
		EpisodeFile struct {
			Path string `json:"path"`
		} `json:"episodeFile"`
	}
	if err := json.Unmarshal(b, &ep); err != nil {
		return "", err
	}
	if ep.EpisodeFile.Path == "" {
		return "", fmt.Errorf("no file for episode %d", episodeID)
	}
	return ep.EpisodeFile.Path, nil
}
