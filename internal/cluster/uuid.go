package cluster

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
)

var clusterUUIDNamespace = []byte("0f6c4f02-df9f-4d8d-a383-0c6e4b7a9d43")

func EnsureOAuthPayloadUUID(raw []byte) ([]byte, string, bool, error) {
	var payload map[string]any
	if errUnmarshal := json.Unmarshal(raw, &payload); errUnmarshal != nil {
		return nil, "", false, errUnmarshal
	}
	if payload == nil {
		return nil, "", false, fmt.Errorf("oauth payload must be a JSON object")
	}

	if rawUUID, ok := payload["uuid"]; ok {
		uuidValue, okString := rawUUID.(string)
		if !okString {
			return nil, "", false, fmt.Errorf("oauth payload uuid must be a string")
		}
		trimmedUUID := strings.TrimSpace(uuidValue)
		if trimmedUUID == "" {
			return nil, "", false, fmt.Errorf("oauth payload uuid is empty")
		}
		if !isValidUUID(trimmedUUID) {
			return nil, "", false, fmt.Errorf("oauth payload uuid is invalid")
		}
		return raw, trimmedUUID, false, nil
	}

	generatedUUID, errRandomUUID := randomUUID()
	if errRandomUUID != nil {
		return nil, "", false, errRandomUUID
	}
	payload["uuid"] = generatedUUID
	updatedRaw, errMarshal := json.MarshalIndent(payload, "", "  ")
	if errMarshal != nil {
		return nil, "", false, errMarshal
	}
	updatedRaw = append(updatedRaw, '\n')
	return updatedRaw, generatedUUID, true, nil
}

func DeterministicAPIKeyUUID(provider, baseURL, apiKeyHash, compatName, providerKey string) string {
	input := strings.Join([]string{
		canonicalLower(provider),
		canonicalLower(baseURL),
		canonicalLower(apiKeyHash),
		canonicalLower(compatName),
		canonicalLower(providerKey),
	}, "\x00")
	return deterministicUUID(input)
}

func DeterministicVirtualUUID(parentUUID, projectID string) string {
	input := strings.Join([]string{strings.TrimSpace(parentUUID), strings.TrimSpace(projectID)}, "\x00")
	return deterministicUUID(input)
}

func NormalizeClusterAuth(auth *coreauth.Auth, uuidValue string) {
	if auth == nil {
		return
	}
	trimmedUUID := strings.TrimSpace(uuidValue)
	if trimmedUUID == "" {
		return
	}
	auth.ID = trimmedUUID
	auth.Index = trimmedUUID
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	auth.Attributes["cluster_uuid"] = trimmedUUID
	if auth.Metadata != nil {
		auth.Metadata["uuid"] = trimmedUUID
	}
}

func APIKeyHash(apiKey string) string {
	trimmedKey := strings.TrimSpace(apiKey)
	if trimmedKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmedKey))
	return hex.EncodeToString(sum[:])
}

func canonicalLower(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func randomUUID() (string, error) {
	var raw [16]byte
	if _, errReadFull := rand.Read(raw[:]); errReadFull != nil {
		return "", errReadFull
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return formatUUID(raw[:]), nil
}

func deterministicUUID(input string) string {
	seed := append([]byte{}, clusterUUIDNamespace...)
	seed = append(seed, 0)
	seed = append(seed, []byte(input)...)
	sum := sha256.Sum256(seed)
	raw := sum[:16]
	raw[6] = (raw[6] & 0x0f) | 0x50
	raw[8] = (raw[8] & 0x3f) | 0x80
	return formatUUID(raw)
}

func formatUUID(raw []byte) string {
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], raw[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], raw[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], raw[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], raw[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], raw[10:16])
	return string(dst)
}

func isValidUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, char := range value {
		switch i {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !isHexChar(char) {
				return false
			}
		}
	}
	return true
}

func isHexChar(char rune) bool {
	return (char >= '0' && char <= '9') ||
		(char >= 'a' && char <= 'f') ||
		(char >= 'A' && char <= 'F')
}
