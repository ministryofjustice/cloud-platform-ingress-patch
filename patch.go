package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v48/github"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

type repository struct {
	Name      string
	LocalRepo *git.Repository
	Reference *plumbing.Reference
	Worktree  *git.Worktree
	Template  *template
}

type template struct {
	Ing ingress
	Dep deployment
	Svc service
}

type manifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

type deployment struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Replicas int `yaml:"replicas"`
		Selector struct {
			MatchLabels struct {
				App string `yaml:"app"`
			} `yaml:"matchLabels"`
		} `yaml:"selector"`
		Template struct {
			Metadata struct {
				Labels struct {
					App string `yaml:"app"`
				} `yaml:"labels"`
			} `yaml:"metadata"`
			Spec struct {
				Containers []struct {
					Name  string `yaml:"name"`
					Image string `yaml:"image"`
					Env   []struct {
						Name      string `yaml:"name"`
						ValueFrom struct {
							SecretKeyRef struct {
								Name string `yaml:"name"`
								Key  string `yaml:"key"`
							} `yaml:"secretKeyRef"`
						} `yaml:"valueFrom"`
					} `yaml:"env"`
					Ports []struct {
						ContainerPort int `yaml:"containerPort"`
					} `yaml:"ports"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type service struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name   string `yaml:"name"`
		Labels struct {
			App string `yaml:"app"`
		} `yaml:"labels"`
	} `yaml:"metadata"`
	Spec struct {
		Ports []struct {
			Port       int    `yaml:"port"`
			Name       string `yaml:"name"`
			TargetPort int    `yaml:"targetPort"`
		} `yaml:"ports"`
		Selector struct {
			App string `yaml:"app"`
		} `yaml:"selector"`
	} `yaml:"spec"`
}

type ingress struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name        string `yaml:"name"`
		Annotations struct {
			ExternalDNSAlphaKubernetesIoSetIdentifier string `yaml:"external-dns.alpha.kubernetes.io/set-identifier"`
			ExternalDNSAlphaKubernetesIoAwsWeight     string `yaml:"external-dns.alpha.kubernetes.io/aws-weight"`
		} `yaml:"annotations"`
	} `yaml:"metadata"`
	Spec struct {
		IngressClassName string `yaml:"ingressClassName"`
		TLS              []struct {
			Hosts []string `yaml:"hosts"`
		} `yaml:"tls"`
		Rules []struct {
			Host string `yaml:"host"`
			HTTP struct {
				Paths []struct {
					Path     string `yaml:"path"`
					PathType string `yaml:"pathType"`
					Backend  struct {
						Service struct {
							Name string `yaml:"name"`
							Port struct {
								Number int `yaml:"number"`
							} `yaml:"port"`
						} `yaml:"service"`
					} `yaml:"backend"`
				} `yaml:"paths"`
			} `yaml:"http"`
		} `yaml:"rules"`
	} `yaml:"spec"`
}

func main() {
	const (
		base       = "https://github.com/ministryofjustice/"
		newApi     = "networking.k8s.io/v1"
		oldApi     = "networking.k8s.io/v1beta1"
		message    = "Update ingress apiVersion to networking.k8s.io/v1 and format yaml"
		branchName = "ingress-patch"
		fileName   = "kubernetes-deploy.tpl"
		tempDir    = "./tmp/"
	)

	var (
		user = flag.String("u", "", "github username")
		pass = flag.String("p", "", "github password")
	)

	flag.Parse()

	// create the github client
	client, err := createGitHubClient(*pass)
	if err != nil {
		log.Fatal(err)
	}

	// define a slice of repositories to patch
	repos := []string{
		// "apply-for-compensation-prototype",
		// "apply-for-legal-aid-prototype",
		// "book-a-prison-visit-prototype",
		// "dex-ia-proto",
		// "eligibility-estimate",
		// "hmpps-assess-risks-and-needs-prototypes",
		// "hmpps-incentives-tool",
		// "hmpps-interventions-prototype",
		// "hmpps-licenses-prototype",
		// "hmpps-manage-supervisions-prototype",
		// "hmpps-prepare-a-case-prototype",
		// "hmpps-prisoner-education",
		// "interventions-design-history",
		"jason-design-demo",
		// "laa-crime-apply-prototype",
		// "laa-view-court-data-prototype",
		// "makerecall-prototype",
		// "manage-supervisions-design-history",
		// "opg-lpa-fd-prototype",
		// "opg-sirius-prototypes",
		// "request-info-from-moj",
		// "send-legal-mail-prototype",
	}

	if err := prepareTempDir(tempDir); err != nil {
		log.Fatal(err)
	}
	defer func() {
		err = cleanTempDir(tempDir)
		if err != nil {
			log.Println(err)
		}
	}()

	repositories, err := createRepositories(base, *user, *pass, repos)
	if err != nil {
		log.Printf("error creating repositories: %v", err)
	}

	g := new(errgroup.Group)
	for _, repo := range repositories {
		repo := repo
		g.Go(func() error {
			fmt.Println("cloning " + repo.Name)
			if err = repo.Checkout(branchName); err != nil {
				return fmt.Errorf("error checking out branch on repo %s: %v", repo.Name, err)
			}

			kFile := filepath.Join(repo.Name, fileName)
			b, err := os.ReadFile(kFile)
			if err != nil {
				return fmt.Errorf("error reading file: %v", err)
			}
			if err := repo.PatchIngress(newApi, b); err != nil {
				return fmt.Errorf("error patching ingress on repo %s: %v", repo.Name, err)
			}

			file, err := createKubernetesDeploy(kFile)
			if err != nil {
				return fmt.Errorf("error creating kubernetes deploy file on repo %s: %v", repo.Name, err)
			}
			defer file.Close()

			if err := repo.writeYaml(file); err != nil {
				return fmt.Errorf("error writing yaml: %v", err)
			}

			if err := repo.createPullRequest(*user, *pass, message, branchName, client); err != nil {
				return fmt.Errorf("error creating pull request on repo %s: %v", repo.Name, err)
			}
			return err
		})
	}
	if err := g.Wait(); err != nil {
		log.Println(err)
	} else {
		fmt.Println("finished")
	}
}

func cleanTempDir(dir string) error {
	err := os.Chdir("..")
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func createRepositories(base, user, pass string, repos []string) ([]repository, error) {
	var repositories []repository
	for _, repo := range repos {
		repository, err := NewRepository(base, repo, user, pass)
		if err != nil {
			return nil, fmt.Errorf("failed to clone %s: %v", repo, err)
		}
		repositories = append(repositories, *repository)
	}
	return repositories, nil
}

func createGitHubClient(pass string) (*github.Client, error) {
	if pass == "" {
		return nil, fmt.Errorf("no github token provided")
	}
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: pass},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)
	return client, nil
}

func prepareTempDir(path string) error {
	// loop through repositories and git clone
	err := os.Mkdir(path, 0755)
	if err != nil {
		return err
	}

	err = os.Chdir(path)
	if err != nil {
		return err
	}

	return nil
}

func NewRepository(base, repo, user, pass string) (*repository, error) {
	localRepo, err := git.PlainClone(repo, false, &git.CloneOptions{
		URL: base + repo,
		Auth: &http.BasicAuth{
			Username: user,
			Password: pass,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cloning error %s: %v", repo, err)
	}

	// Get HEAD ref from repository
	ref, err := localRepo.Head()
	if err != nil {
		return nil, err
	}

	// Get the worktree for the local repository
	tree, err := localRepo.Worktree()
	if err != nil {
		return nil, err
	}

	t := &template{}

	return &repository{
		Name:      repo,
		LocalRepo: localRepo,
		Reference: ref,
		Worktree:  tree,
		Template:  t,
	}, nil
}

func (r *repository) Checkout(branch string) error {
	err := r.Worktree.Checkout(&git.CheckoutOptions{
		Hash:   r.Reference.Hash(),
		Branch: plumbing.NewBranchReferenceName(branch),
		Create: true,
	})
	if err != nil {
		return err
	}

	return nil
}

func (r *repository) PatchIngress(newApi string, yamlFile []byte) error {
	allByteSlices, err := splitYAML(yamlFile)
	if err != nil {
		return fmt.Errorf("failed to split yaml for %s: %v", r.Name, err)
	}
	for _, byteSlice := range allByteSlices {
		var t manifest
		err := yaml.Unmarshal(byteSlice, &t)
		if err != nil {
			return fmt.Errorf("failed to unmarshal yaml for %s: %v", r.Name, err)
		}
		// case switch to create a deployment, service and ingress
		switch t.Kind {
		case "Ingress":
			err := yaml.Unmarshal(byteSlice, &r.Template.Ing)
			if err != nil {
				return fmt.Errorf("failed to unmarshal yaml for %s: %v", r.Name, err)
			}
		case "Deployment":
			err := yaml.Unmarshal(byteSlice, &r.Template.Dep)
			if err != nil {
				return fmt.Errorf("failed to unmarshal yaml for %s: %v", r.Name, err)
			}
		case "Service":
			err := yaml.Unmarshal(byteSlice, &r.Template.Svc)
			if err != nil {
				return fmt.Errorf("failed to unmarshal yaml for %s: %v", r.Name, err)
			}
		}

	}

	// Move ingress to the new controller
	r.Template.Ing.Spec.IngressClassName = "default"
	// Change the ingress API to the new version
	r.Template.Ing.APIVersion = newApi
	// Add specfic pathtype
	r.Template.Ing.Spec.Rules[0].HTTP.Paths[0].PathType = "ImplementationSpecific"

	// Add new service data
	r.Template.Ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name = r.Template.Svc.Metadata.Name
	r.Template.Ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Number = r.Template.Svc.Spec.Ports[0].Port

	return nil
}

func createKubernetesDeploy(filePath string) (*os.File, error) {
	err := os.Remove(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to remove file for %s: %v", filePath, err)
	}

	// write the new ingress to the file
	f, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file for %s: %v", filePath, err)
	}

	return f, nil
}

func (r *repository) writeYaml(f *os.File) error {
	// write the new ingress to the file
	templates := []any{
		r.Template.Dep,
		r.Template.Svc,
		r.Template.Ing,
	}

	for _, template := range templates {
		y, err := yaml.Marshal(template)
		if err != nil {
			return fmt.Errorf("failed to marshal yaml for %s: %v", r.Name, err)
		}
		// write any to a file
		_, err = f.Write(y)
		if err != nil {
			return fmt.Errorf("failed to write to file for %s: %v", r.Name, err)
		}
		_, err = f.WriteString("\n---\n\n")
		if err != nil {
			return fmt.Errorf("failed to write to file for %s: %v", r.Name, err)
		}
	}

	return nil
}

func stageChanges(repo string, worktree *git.Worktree) error {
	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to get status for %s: %v", repo, err)
	}

	if status.IsClean() {
		log.Printf("no changes for %s", repo)
	}

	for path := range status {
		if status.IsUntracked(path) {
			_, err := worktree.Add(path)
			if err != nil {
				log.Printf("failed to add file for %s: %v", repo, err)
			}
		}
	}

	return nil
}

func (r *repository) createPullRequest(user, password, message, branchName string, client *github.Client) error {
	if err := stageChanges(r.Name, r.Worktree); err != nil {
		return fmt.Errorf("failed to stage changes for %s: %v", r.Name, err)
	}

	if err := commitChanges(user, password, r.Name, r.LocalRepo, r.Worktree, message); err != nil {
		return fmt.Errorf("failed to commit changes for %s: %v", r.Name, err)
	}

	if err := prChanges(r.Name, message, branchName, *client); err != nil {
		return fmt.Errorf("failed to push changes for %s: %v", r.Name, err)
	}

	return nil

}

func prChanges(repo, message, branchName string, client github.Client) error {
	createPR := &github.NewPullRequest{
		Title: github.String(message),
		Head:  github.String(string(branchName)),
		Base:  github.String("main"),
	}

	_, _, err := client.PullRequests.Create(context.Background(), "ministryofjustice", repo, createPR)
	if err != nil {
		return fmt.Errorf("failed to create pull request for %s: %v", repo, err)
	}

	return nil
}

func commitChanges(user, password, repo string, localRepo *git.Repository, worktree *git.Worktree, message string) error {
	_, err := worktree.Commit(message, &git.CommitOptions{
		All: true,
	})
	if err != nil {
		log.Printf("failed to commit for %s: %v", repo, err)
	}

	err = localRepo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth: &http.BasicAuth{
			Username: user,
			Password: password,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to push for %s: %v", repo, err)
	}

	return nil
}

func splitYAML(resources []byte) ([][]byte, error) {
	dec := yaml.NewDecoder(bytes.NewReader(resources))

	var res [][]byte
	for {
		var value any
		err := dec.Decode(&value)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		valueBytes, err := yaml.Marshal(value)
		if err != nil {
			return nil, err
		}
		res = append(res, valueBytes)
	}
	return res, nil
}
