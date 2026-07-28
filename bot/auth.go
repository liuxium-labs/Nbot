package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/auth"
	"golang.org/x/oauth2"
)

const tokenDir = "tokens"

var LogWriter io.Writer = os.Stdout

type savedToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

func HasToken(botName string) bool {
	_, err := os.Stat(tokenPath(botName))
	return err == nil
}

func tokenPath(botName string) string {
	return filepath.Join(tokenDir, botName+".json")
}

func LoadOrAuth(botName string, logW io.Writer) (oauth2.TokenSource, error) {
	if err := os.MkdirAll(tokenDir, 0700); err != nil {
		return nil, err
	}

	path  :=  tokenPath(botName)
	data, err  := os.ReadFile(path)
	if err == nil {
		var st savedToken
		if json.Unmarshal(data, &st) == nil {
			tok := &oauth2.Token{
				AccessToken:  st.AccessToken,
				TokenType:    st.TokenType,
				RefreshToken: st.RefreshToken,
				Expiry:       st.Expiry,
			}
			fmt.Fprintf(logW, "%s: loaded saved token (expires %s)\n", botName, st.Expiry.Format("2006-01-02 15:04"))
			return auth.RefreshTokenSourceWriter(tok, logW), nil
		}
	}

	fmt.Fprintf(logW, "%s: no saved token, starting Xbox auth...\n", botName)
	tok, err := auth.RequestLiveTokenWriter(logW)
	if err != nil {
		return nil, fmt.Errorf("xbox auth: %w", err)
	}

	if err := saveToken(botName, tok); err != nil {
		fmt.Fprintf(logW, "%s: warning, could not save token: %v\n", botName, err)
	} else {
		fmt.Fprintf(logW, "%s: token saved to %s\n", botName, path)
	}

	return auth.RefreshTokenSource(tok), nil
}

func saveToken(botName string, tok *oauth2.Token) error {

	st := savedToken{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tokenPath(botName), data, 0600)
}
