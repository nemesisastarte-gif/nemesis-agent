package v1

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/teteekoue/NemesisCode/backend/domain"
)

// encodeFrontendUserInput reproduit le format exact du frontend
// (task-stream-client.ts sendInitialUserInput) :
// data = b64(JSON{content: b64(texte), attachments}).
func encodeFrontendUserInput(text string) []byte {
	payload := map[string]any{
		"content":     base64.StdEncoding.EncodeToString([]byte(text)),
		"attachments": []domain.TaskAttachment{},
	}
	inner, _ := json.Marshal(payload)
	return []byte(base64.StdEncoding.EncodeToString(inner))
}

func TestParseUserInputDataFrontendFormat(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"ascii", "ajoute une ligne au fichier"},
		{"accentue", "Crée un fichier hello.txt"},
		{"emojis", "fait un résumé 🚀"},
		{"base64-like", "TWFuIGlzIGRpc3Rpbmd1aXNoZWQ"}, // texte clair qui est du base64 valide
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := parseUserInputData(encodeFrontendUserInput(tc.text))
			if got := string(req.Content); got != tc.text {
				t.Fatalf("Content = %q, want %q", got, tc.text)
			}
		})
	}
}

func TestParseUserInputDataPlainText(t *testing.T) {
	// Message initial / historique : contenu déjà en clair.
	req := parseUserInputData([]byte("Crée un fichier hello.txt"))
	if got := string(req.Content); got != "Crée un fichier hello.txt" {
		t.Fatalf("Content = %q, want plain text unchanged", got)
	}
}

func TestParseUserInputDataPlainJSON(t *testing.T) {
	// Format stocké : JSON en clair avec content en base64.
	inner := map[string]any{
		"content":     base64.StdEncoding.EncodeToString([]byte("bonjour")),
		"attachments": []domain.TaskAttachment{},
	}
	raw, _ := json.Marshal(inner)
	req := parseUserInputData(raw)
	if got := string(req.Content); got != "bonjour" {
		t.Fatalf("Content = %q, want %q", got, "bonjour")
	}
}

func TestParseUserInputDataStorageFormat(t *testing.T) {
	// Format de stockage : encoding=plaintext.
	stored := map[string]any{
		"encoding":    "plaintext",
		"content":     "du texte clair",
		"attachments": []domain.TaskAttachment{},
	}
	raw, _ := json.Marshal(stored)
	req := parseUserInputData(raw)
	if got := string(req.Content); got != "du texte clair" {
		t.Fatalf("Content = %q, want %q", got, "du texte clair")
	}
}

func TestParseUserInputDataBase64OnlyFallsBackToRaw(t *testing.T) {
	// Base64 valide mais qui ne forme pas le JSON attendu : on ne doit pas
	// altérer le contenu (un texte en clair type "TWFu" reste intact).
	req := parseUserInputData([]byte("TWFu"))
	if got := string(req.Content); got != "TWFu" {
		t.Fatalf("Content = %q, want %q (no corruption of base64-like plain text)", got, "TWFu")
	}
}
