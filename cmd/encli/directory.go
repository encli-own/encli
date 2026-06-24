package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/encli-own/encli/pkg/crypto"
)

const directoryVersion = "encli-directory-v1"

func computeBlindedID(nickname string) string {
	normalized := strings.ToLower(strings.TrimSpace(nickname))
	mac := hmac.New(sha256.New, []byte(directoryVersion))
	mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

func deriveEncKey(nickname string) []byte {
	normalized := strings.ToLower(strings.TrimSpace(nickname))
	h := sha256.Sum256([]byte("encli-dir-enc:" + normalized))
	return h[:]
}

func encryptProfile(nickname, deviceID string) (string, error) {
	key := deriveEncKey(nickname)
	ciph, err := crypto.NewCipher(key)
	if err != nil {
		return "", err
	}
	payload := fmt.Sprintf(`{"nickname":"%s","device_id":"%s"}`, nickname, deviceID)
	enc, err := ciph.Encrypt([]byte(payload), nil)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(enc), nil
}

func decryptProfile(nickname, encryptedHex string) (map[string]string, error) {
	key := deriveEncKey(nickname)
	ciph, err := crypto.NewCipher(key)
	if err != nil {
		return nil, err
	}
	enc, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return nil, err
	}
	data, err := ciph.Decrypt(enc, nil)
	if err != nil {
		return nil, err
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if !strings.EqualFold(result["nickname"], nickname) {
		return nil, fmt.Errorf("nickname mismatch")
	}
	return result, nil
}

func publishProfile(serverAddr, nickname string) error {
	identity, err := loadOrCreateIdentity()
	if err != nil {
		return err
	}

	profile, err := encryptProfile(nickname, identity.DeviceID)
	if err != nil {
		return fmt.Errorf("encrypting profile: %w", err)
	}

	blindedID := computeBlindedID(nickname)
	fmt.Printf("Publishing profile for nickname '%s'...\n", nickname)
	fmt.Printf("  Blinded ID: %s\n", blindedID[:16]+"...")

	url := fmt.Sprintf("http://%s/v1/directory/publish", serverAddr)
	body := map[string]string{
		"nickname":          nickname,
		"encrypted_profile": profile,
	}
	bodyBytes, _ := json.Marshal(body)

	resp, err := http.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("publish request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
		Error   string                 `json:"error"`
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err = dec.Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("server error: %s", result.Error)
	}

	_ = result.Data["blinded_id"]

	if err := saveNickname(nickname); err != nil {
		return fmt.Errorf("saving nickname locally: %w", err)
	}

	fmt.Printf("  Success! Profile published.\n")
	fmt.Printf("  Others can find you by searching for '%s'\n", nickname)
	return nil
}

func lookupProfile(serverAddr, nickname string) error {
	blindedID := computeBlindedID(nickname)
	fmt.Printf("Looking up profile for '%s'...\n", nickname)

	url := fmt.Sprintf("http://%s/v1/directory/lookup?blinded_id=%s", serverAddr, blindedID)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("lookup request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
		Error   string                 `json:"error"`
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err = dec.Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("profile not found for '%s'", nickname)
	}

	profileEnc, _ := result.Data["profile"].(string)
	profile, err := decryptProfile(nickname, profileEnc)
	if err != nil {
		return fmt.Errorf("decryption failed (wrong nickname?): %w", err)
	}

	fmt.Printf("\n")
	fmt.Printf("╔══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║              PROFILE FOUND                               ║\n")
	fmt.Printf("╠══════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║ Nickname:     %-42s ║\n", profile["nickname"])
	fmt.Printf("║ Device ID:    %-42s ║\n", profile["device_id"])
	fmt.Printf("╚══════════════════════════════════════════════════════════╝\n")

	return nil
}

func searchProfiles(serverAddr, query string) error {
	queryID := computeBlindedID(query)
	prefix := queryID[:16]
	fmt.Printf("Searching for profiles matching '%s'...\n", query)

	url := fmt.Sprintf("http://%s/v1/directory/search?q=%s", serverAddr, prefix)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
		Error   string                 `json:"error"`
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err = dec.Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("server error: %s", result.Error)
	}

	rawResults, _ := result.Data["results"].([]interface{})
	count, _ := result.Data["count"].(json.Number)
	countInt, _ := count.Int64()

	if countInt == 0 || len(rawResults) == 0 {
		fmt.Printf("No profiles found matching '%s'\n", query)
		return nil
	}

	fmt.Printf("\nFound %d profile(s):\n\n", countInt)
	for i, raw := range rawResults {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		profileEnc, _ := entry["profile"].(string)
		profile, err := decryptProfile(query, profileEnc)
		if err != nil {
			continue
		}
		shortID := profile["device_id"]
		if len(shortID) > 16 {
			shortID = shortID[:16]
		}
		fmt.Printf("  %d. %s (Device: %s)\n", i+1, profile["nickname"], shortID+"...")
	}

	return nil
}

func searchDirectory(serverAddr, nickname string) string {
	blindedID := computeBlindedID(nickname)
	url := fmt.Sprintf("http://%s/v1/directory/lookup?blinded_id=%s", serverAddr, blindedID)
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
		Error   string                 `json:"error"`
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err = dec.Decode(&result); err != nil {
		return ""
	}
	if !result.Success {
		return ""
	}

	profileEnc, _ := result.Data["profile"].(string)
	profile, err := decryptProfile(nickname, profileEnc)
	if err != nil {
		return ""
	}
	return profile["device_id"]
}
