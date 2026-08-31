package credentials

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/99designs/keyring"
	"github.com/manifoldco/promptui"
)

type Keyring struct {
	kr keyring.Keyring
}

var _ CredentialGetter = &Keyring{}

func (k Keyring) CredentialForHost(h string) (*Credentials, error) {
	secretKey := fmt.Sprintf("%s-credentials", h)
	v, err := k.kr.Get(secretKey)
	creds := &Credentials{}
	if err == nil {
		err = json.NewDecoder(bytes.NewReader(v.Data)).Decode(creds)
		if err != nil {
			return nil, err
		}
	}

	if err == keyring.ErrKeyNotFound {
		prompt := promptui.Prompt{
			Label:  fmt.Sprintf("username for %s", h),
			Stdin:  os.Stdin,
			Stdout: os.Stderr,
		}
		creds.Username, err = prompt.Run()
		if err != nil {
			return nil, err
		}
		prompt = promptui.Prompt{
			Label:  fmt.Sprintf("password for %s (usually your Personal Access Token)", h),
			Mask:   '*',
			Stdin:  os.Stdin,
			Stdout: os.Stderr,
		}
		creds.Password, err = prompt.Run()
		if err != nil {
			return nil, err
		}
		d := bytes.NewBuffer(nil)
		err = json.NewEncoder(d).Encode(creds)
		if err != nil {
			return creds, nil
		}
		err = k.kr.Set(keyring.Item{
			Key:         secretKey,
			Label:       h,
			Description: fmt.Sprintf("json encoded user/password to access %s", h),
			Data:        d.Bytes(),
		})
		if err != nil {
			return creds, nil
		}
	}
	if err != nil {
		return nil, err
	}

	return creds, nil
}

func NewKeyring(cfg keyring.Config) (CredentialGetter, error) {
	kr, err := keyring.Open(cfg)
	if err != nil {
		return nil, err
	}
	return &Keyring{kr: kr}, nil
}

func MustNewKeyring(cfg keyring.Config) CredentialGetter {
	c, err := NewKeyring(cfg)
	if err != nil {
		panic(err)
	}
	return c
}
