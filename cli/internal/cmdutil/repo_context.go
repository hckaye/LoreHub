package cmdutil

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/lorehub/lorehub/cli/internal/config"
)

// RepoContext is the repository and host selected for a command.
type RepoContext struct {
	Host  string
	Owner string
	Name  string
}

func (repo RepoContext) String() string {
	return repo.Owner + "/" + repo.Name
}

func (repo RepoContext) apiPath(suffix string) string {
	return "/api/v1/repositories/" + url.PathEscape(repo.Owner) + "/" +
		url.PathEscape(repo.Name) + suffix
}

// ResolveRepo returns the first repository selected by the ADR precedence:
// command flag, LH_REPO, then the stored default.
func ResolveRepo(explicit string, environment string, storedDefault string) (string, error) {
	repository, err := resolveRepoContext(explicit, environment, storedDefault)
	if err != nil {
		return "", err
	}
	return repository.String(), nil
}

func resolveRepoContext(explicit string, environment string, storedDefault string) (RepoContext, error) {
	for _, value := range []string{explicit, environment, storedDefault} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		repository, err := ParseRepoContext(value)
		if err != nil {
			return RepoContext{}, err
		}
		return repository, nil
	}
	return RepoContext{}, fmt.Errorf("repository is required; use --repo OWNER/NAME or set LH_REPO")
}

func ParseRepoContext(value string) (RepoContext, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return RepoContext{}, fmt.Errorf("repository must be OWNER/NAME")
	}

	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return RepoContext{}, fmt.Errorf("repository host is invalid")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return RepoContext{}, fmt.Errorf("repository host must use http or https")
		}
		parts := splitRepoPath(parsed.Path)
		if len(parts) != 2 {
			return RepoContext{}, fmt.Errorf("repository must be [HOST/]OWNER/NAME")
		}
		return validRepoContext(RepoContext{
			Host:  parsed.Scheme + "://" + parsed.Host,
			Owner: parts[0],
			Name:  parts[1],
		})
	}

	parts := strings.Split(value, "/")
	if len(parts) != 2 && len(parts) != 3 {
		return RepoContext{}, fmt.Errorf("repository must be [HOST/]OWNER/NAME")
	}
	repository := RepoContext{Owner: parts[len(parts)-2], Name: parts[len(parts)-1]}
	if len(parts) == 3 {
		repository.Host = parts[0]
	}
	return validRepoContext(repository)
}

func splitRepoPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func validRepoContext(repository RepoContext) (RepoContext, error) {
	if repository.Owner == "" || repository.Name == "" ||
		strings.ContainsAny(repository.Owner, " \t\r\n/?#") ||
		strings.ContainsAny(repository.Name, " \t\r\n/?#") {
		return RepoContext{}, fmt.Errorf("repository must be [HOST/]OWNER/NAME")
	}
	if strings.Contains(repository.Host, "://") {
		parsed, err := url.Parse(repository.Host)
		if err != nil || parsed.Host == "" || parsed.Path != "" ||
			parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
			return RepoContext{}, fmt.Errorf("repository host is invalid")
		}
	} else if strings.ContainsAny(repository.Host, " \t\r\n/?#") {
		return RepoContext{}, fmt.Errorf("repository host is invalid")
	}
	return repository, nil
}

func (state *rootState) resolveRepo() (RepoContext, error) {
	hosts, err := state.loadHosts()
	if err != nil {
		return RepoContext{}, err
	}

	host := state.repoHost(hosts)
	entry, _ := state.selectedHostEntry(hosts, host)
	repository, err := resolveRepoContext(state.repoFlag, os.Getenv("LH_REPO"), entry.DefaultRepo)
	if err != nil {
		return RepoContext{}, err
	}
	if repository.Host == "" {
		repository.Host = host
	}
	return repository, nil
}

func (state *rootState) repoHost(hosts config.Hosts) string {
	if strings.TrimSpace(state.hostFlag) != "" || strings.TrimSpace(os.Getenv("LH_HOST")) != "" ||
		strings.TrimSpace(state.defaultHost) != config.DefaultHost || len(hosts) != 1 {
		return state.host()
	}
	hostsList := make([]string, 0, len(hosts))
	for host := range hosts {
		hostsList = append(hostsList, host)
	}
	sort.Strings(hostsList)
	return hostsList[0]
}
