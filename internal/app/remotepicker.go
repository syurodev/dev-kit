package app

import (
	"context"

	"devkit/internal/accounts"
	gitservice "devkit/internal/git"
	"devkit/internal/interaction"
)

// remotePicker answers "which account authenticates this git remote" for a
// remote with no binding of its own: Accounts decides when it can, and the
// person is asked, through the interaction hub, when it cannot.
//
// It is the only place the two meet. git declares the question as an interface
// so it imports neither, and Accounts does not know the hub exists.
type remotePicker struct {
	accounts *accounts.Service
	hub      *interaction.Hub
}

func (p remotePicker) PickAccount(ctx context.Context, in gitservice.PickAccountInput) (string, error) {
	// An SSH remote is authenticated by a key and an HTTPS one by a token, so
	// the two are different questions with different candidates and different
	// remembered answers.
	via := viaOf(in.Transport)
	// An ssh alias already says which identity the remote is for: the person
	// wrote it into ~/.ssh/config for exactly that. It answers before the owner
	// of the repository is asked about.
	if in.Alias != "" {
		if account, ok := p.accounts.ForAlias(in.Alias); ok {
			return account.ID, nil
		}
	}
	match, err := p.accounts.ForRemote(in.Host, in.Owner, in.Env, via)
	if err != nil {
		return "", err
	}
	if match.Decided {
		return match.Account.ID, nil
	}
	if len(match.Options) == 0 {
		// No account here fits this host: git's own configuration answers.
		return "", nil
	}
	if p.hub == nil {
		return "", interaction.ErrSelectionRequired
	}

	options := make([]interaction.AccountOption, 0, len(match.Options))
	for _, account := range match.Options {
		options = append(options, interaction.AccountOption{
			ID: account.ID, Label: account.Label, Provider: account.Provider, Subject: account.Subject,
		})
	}
	remember := ""
	if in.Owner != "" {
		remember = in.Owner + " on " + in.Host
	}
	choice, err := p.hub.SelectAccount(ctx, interaction.SelectAccountRequest{
		Summary: remoteQuestion(in), Remember: remember, Options: options,
	})
	if err != nil {
		return "", err
	}
	if choice.Remember {
		// A failure to remember costs one more question next time; it must not
		// cost the push the person just answered for.
		_ = p.accounts.ChooseForRemote(in.Host, in.Owner, in.Env, via, choice.ID)
	}
	return choice.ID, nil
}

// remoteQuestion words the question in the person's terms: what is about to
// happen, to which repository, on which host.
func remoteQuestion(in gitservice.PickAccountInput) string {
	repository := in.Repo
	if in.Owner != "" {
		repository = in.Owner + "/" + in.Repo
	}
	switch in.Op {
	case "push":
		return "Push " + repository + " to " + in.Host
	case "pull":
		return "Pull " + repository + " from " + in.Host
	default:
		return "Fetch " + repository + " from " + in.Host
	}
}

// viaOf is how a remote is reached, as Accounts names it.
func viaOf(transport gitservice.Transport) accounts.Via {
	if transport == gitservice.TransportSSH {
		return accounts.ViaSSH
	}
	return accounts.ViaHTTPS
}

// IdentityFor says who the account for a remote commits as, without asking
// anyone: a commit is quiet, and a dialog in the middle of one is not added
// here. An explicit binding wins; otherwise only an account Accounts has
// already decided (the only one that fits, or one chosen and remembered) counts,
// and "two fit and none was chosen yet" is no account — the question waits for
// the first push.
func (p remotePicker) IdentityFor(_ context.Context, in gitservice.PickAccountInput, boundAccountID string) (gitservice.AccountIdentity, error) {
	id := boundAccountID
	if id == "" && in.Alias != "" {
		if account, ok := p.accounts.ForAlias(in.Alias); ok {
			id = account.ID
		}
	}
	if id == "" {
		match, err := p.accounts.ForRemote(in.Host, in.Owner, in.Env, viaOf(in.Transport))
		if err != nil {
			return gitservice.AccountIdentity{}, err
		}
		if !match.Decided {
			return gitservice.AccountIdentity{}, nil
		}
		id = match.Account.ID
	}
	account, err := p.accounts.Get(id)
	if err != nil {
		return gitservice.AccountIdentity{}, err
	}
	identity, _ := p.accounts.IdentityOf(id)
	return gitservice.AccountIdentity{Name: identity.Name, Email: identity.Email, Account: account.Label}, nil
}
