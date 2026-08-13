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
	for _, value := range []string{explicit, environment, storedDefault} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		repository, err := ParseRepoContext(value)
		if err != nil {
			return "", err
		}
		return repository.String(), nil
	}
	return "", fmt.Errorf("repository is required; use --repo OWNER/NAME or set LH_REPO")
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

	explicit := strings.TrimSpace(state.repoFlag)
	environment := strings.TrimSpace(os.Getenv("LH_REPO"))
	selected := explicit
	if selected == "" {
		selected = environment
	}
	if selected != "" {
		repository, err := ParseRepoContext(selected)
		if err != nil {
			return RepoContext{}, err
		}
		if repository.Host == "" {
			repository.Host = state.repoHost(hosts)
		}
		return repository, nil
	}

	host := state.repoHost(hosts)
	entry, found := state.selectedHostEntry(hosts, host)
	if !found || strings.TrimSpace(entry.DefaultRepo) == "" {
		return RepoContext{}, fmt.Errorf("repository is required; use --repo OWNER/NAME or set LH_REPO")
	}
	repository, err := ParseRepoContext(entry.DefaultRepo)
	if err != nil {
		return RepoContext{}, fmt.Errorf("parse stored default repository: %w", err)
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
