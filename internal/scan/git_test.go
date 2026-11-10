package scan

import "testing"

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name, url, host, owner string
	}{
		{"scp-style ssh", "git@github.com:acme/x.git", "github.com", "acme"},
		{"scp-style, no .git", "git@github.com:acme/x", "github.com", "acme"},
		{"ssh://", "ssh://git@github.com/acme/x.git", "github.com", "acme"},
		{"ssh:// with a colon path", "ssh://git@gitlab.com:acme/x.git", "gitlab.com", "acme"},
		{"other host", "git@git.example.org:team/x.git", "git.example.org", "team"},
		{"https", "https://github.com/acme/x.git", "github.com", "acme"},
		{"http", "http://git.local/acme/x", "git.local", "acme"},
		{"https with a user", "https://bob@github.com/acme/x.git", "github.com", "acme"},
		{"https with user and password", "https://bob:s3cret@github.com/acme/x.git", "github.com", "acme"},
		{"CodeCommit https", "https://git-codecommit.us-east-1.amazonaws.com/v1/repos/x", "codecommit", "codecommit"},
		{"CodeCommit ssh", "ssh://APKAEXAMPLE@git-codecommit.eu-west-1.amazonaws.com/v1/repos/x", "codecommit", "codecommit"},
		{"empty", "", "", ""},
		{"garbage", "not a remote", "", ""},
		{"local path", "/srv/git/x.git", "", ""},
		{"file URL", "file:///srv/git/x.git", "", ""},
		{"host only", "https://github.com", "", ""},
		{"owner only, no repo", "git@github.com:acme", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, owner := parseRemote(tc.url)
			if host != tc.host || owner != tc.owner {
				t.Errorf("parseRemote(%q) = (%q, %q), want (%q, %q)", tc.url, host, owner, tc.host, tc.owner)
			}
		})
	}
}
