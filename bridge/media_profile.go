package bridge

import (
	"strconv"
	"strings"
)

type MediaProfile struct {
	HasSDP        bool     `json:"has_sdp"`
	AudioPort     int      `json:"audio_port"`
	AudioProto    string   `json:"audio_proto"`
	PayloadTypes  []string `json:"payload_types"`
	Codecs        []string `json:"codecs"`
	HasPCMU       bool     `json:"has_pcmu"`
	HasPCMA       bool     `json:"has_pcma"`
	HasOpus       bool     `json:"has_opus"`
	RawAudioMLine string   `json:"raw_audio_m_line,omitempty"`
}

func BuildMediaProfile(sdp string) MediaProfile {
	profile := MediaProfile{
		HasSDP:       strings.TrimSpace(sdp) != "",
		PayloadTypes: []string{},
		Codecs:       []string{},
	}
	if !profile.HasSDP {
		return profile
	}

	lines := strings.Split(strings.ReplaceAll(sdp, "\r\n", "\n"), "\n")
	rtpmap := map[string]string{}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "m=audio ") {
			profile.RawAudioMLine = line
			parts := strings.Fields(strings.TrimPrefix(line, "m=audio "))
			if len(parts) >= 2 {
				if p, err := strconv.Atoi(parts[0]); err == nil {
					profile.AudioPort = p
				}
				profile.AudioProto = parts[1]
			}
			if len(parts) >= 3 {
				profile.PayloadTypes = append(profile.PayloadTypes, parts[2:]...)
			}
			continue
		}
		if strings.HasPrefix(line, "a=rtpmap:") {
			rest := strings.TrimPrefix(line, "a=rtpmap:")
			chunks := strings.SplitN(rest, " ", 2)
			if len(chunks) != 2 {
				continue
			}
			pt := strings.TrimSpace(chunks[0])
			codecPart := strings.TrimSpace(chunks[1])
			codec := strings.ToUpper(strings.SplitN(codecPart, "/", 2)[0])
			rtpmap[pt] = codec
		}
	}

	seen := map[string]bool{}
	for _, pt := range profile.PayloadTypes {
		c := strings.ToUpper(strings.TrimSpace(rtpmap[pt]))
		switch {
		case c == "":
			if pt == "0" {
				c = "PCMU"
			} else if pt == "8" {
				c = "PCMA"
			}
		}
		if c == "" {
			continue
		}
		if !seen[c] {
			seen[c] = true
			profile.Codecs = append(profile.Codecs, c)
		}
	}

	for _, codec := range profile.Codecs {
		switch codec {
		case "PCMU":
			profile.HasPCMU = true
		case "PCMA":
			profile.HasPCMA = true
		case "OPUS":
			profile.HasOpus = true
		}
	}
	return profile
}
