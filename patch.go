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
	"gopkg.in/yaml.v3"
)

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
			KubernetesIoIngressClass                  string `yaml:"kubernetes.io/ingress.class"`
			ExternalDNSAlphaKubernetesIoSetIdentifier string `yaml:"external-dns.alpha.kubernetes.io/set-identifier"`
			ExternalDNSAlphaKubernetesIoAwsWeight     string `yaml:"external-dns.alpha.kubernetes.io/aws-weight"`
		} `yaml:"annotations"`
	} `yaml:"metadata"`
	Spec struct {
		TLS []struct {
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

func prepareTempDir() error {
	// loop through repositories and git clone
	err := os.Mkdir("./tmp/", 0755)
	if err != nil {
		return err
	}

	err = os.Chdir("./tmp/")
	if err != nil {
		return err
	}

	return nil
}

type repository struct {
	Name      string
	LocalRepo *git.Repository
	Reference *plumbing.Reference
	Worktree  *git.Worktree
}

func cloneRepository(base, repo, user, pass string) (*repository, error) {
	localRepo, err := git.PlainClone(repo, false, &git.CloneOptions{
		URL: base + repo,
		Auth: &http.BasicAuth{
			Username: user,
			Password: pass,
		},
	})
	if err != nil {
		return nil, err
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

	return &repository{
		Name:      repo,
		LocalRepo: localRepo,
		Reference: ref,
		Worktree:  tree,
	}, nil
}

func (r *repository) checkout(branch string) error {
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

func main() {
	const (
		base       = "https://github.com/ministryofjustice/"
		newApi     = "networking.k8s.io/v1"
		oldApi     = "networking.k8s.io/v1beta1"
		message    = "Update ingress apiVersion to networking.k8s.io/v1 and format yaml"
		branchName = "ingress-patch"
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
		"cloud-platform-prototype-demo",
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
		// "jason-design-demo",
		// "laa-crime-apply-prototype",
		// "laa-view-court-data-prototype",
		// "makerecall-prototype",
		// "manage-supervisions-design-history",
		// "opg-lpa-fd-prototype",
		// "opg-sirius-prototypes",
		// "request-info-from-moj",
		// "send-legal-mail-prototype",
	}

	m := template{}

	if err := prepareTempDir(); err != nil {
		log.Fatal(err)
	}

	for _, repo := range repos {
		fmt.Println("cloning " + repo)
		repository, err := cloneRepository(base, repo, *user, *pass)
		if err != nil {
			log.Printf("failed to clone %s: %v", repo, err)
			continue
		}

		if err = repository.checkout(branchName); err != nil {
			log.Printf("failed to checkout %s: %v", repo, err)
			continue
		}

		kFile := filepath.Join(repo, "kubernetes-deploy.tpl")
		b, err := os.ReadFile(kFile)
		if err != nil {
			log.Printf("failed to read file for %s: %v", repo, err)
		}

		allByteSlices, err := SplitYAML(b)
		if err != nil {
			log.Printf("failed to split yaml for %s: %v", repo, err)
		}
		for _, byteSlice := range allByteSlices {
			// fmt.Printf("Here's a YAML:\n%v\n", string(byteSlice))
			var t manifest
			err := yaml.Unmarshal(byteSlice, &t)
			if err != nil {
				log.Printf("failed to unmarshal yaml for %s: %v", repo, err)
			}
			// case switch to create a deployment, service and ingress
			switch t.Kind {
			case "Ingress":
				err := yaml.Unmarshal(byteSlice, &m.Ing)
				if err != nil {
					log.Printf("failed to unmarshal yaml for %s: %v", repo, err)
				}
			case "Deployment":
				err := yaml.Unmarshal(byteSlice, &m.Dep)
				if err != nil {
					log.Printf("failed to unmarshal yaml for %s: %v", repo, err)
				}
			case "Service":
				err := yaml.Unmarshal(byteSlice, &m.Svc)
				if err != nil {
					log.Printf("failed to unmarshal yaml for %s: %v", repo, err)
				}
			}

		}

		m.Ing.APIVersion = newApi
		m.Ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name = m.Svc.Metadata.Name
		m.Ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Number = m.Svc.Spec.Ports[0].Port

		err = os.Remove(kFile)
		if err != nil {
			log.Printf("failed to remove file for %s: %v", repo, err)
		}

		// write the new ingress to the file
		f, err := os.Create(kFile)
		if err != nil {
			log.Printf("failed to create file for %s: %v", repo, err)
		}
		defer f.Close()

		// write the new ingress to the file
		dep, err := yaml.Marshal(m.Dep)
		if err != nil {
			log.Printf("failed to marshal yaml for %s: %v", repo, err)
		}
		_, err = f.Write(dep)
		if err != nil {
			log.Printf("failed to write to file for %s: %v", repo, err)
		}

		// write a --- to the file
		_, err = f.WriteString("\n---\n\n")
		if err != nil {
			log.Printf("failed to write to file for %s: %v", repo, err)
		}

		// write the new svc to the file
		svc, err := yaml.Marshal(m.Svc)
		if err != nil {
			log.Printf("failed to marshal yaml for %s: %v", repo, err)
		}

		_, err = f.Write(svc)
		if err != nil {
			log.Printf("failed to write to file for %s: %v", repo, err)
		}

		// write a --- to the file
		_, err = f.WriteString("\n---\n\n")
		if err != nil {
			log.Printf("failed to write to file for %s: %v", repo, err)
		}

		// write the new ingress to the file
		ing, err := yaml.Marshal(m.Ing)
		if err != nil {
			log.Printf("failed to marshal yaml for %s: %v", repo, err)
		}

		_, err = f.Write(ing)
		if err != nil {
			log.Printf("failed to write to file for %s: %v", repo, err)
		}

		status, err := repository.Worktree.Status()
		if err != nil {
			log.Printf("failed to get status for %s: %v", repo, err)
		}

		if status.IsClean() {
			log.Printf("no changes for %s", repo)
		}

		for path := range status {
			if status.IsUntracked(path) {
				_, err := repository.Worktree.Add(path)
				if err != nil {
					log.Printf("failed to add file for %s: %v", repo, err)
				}
			}
		}

		_, err = repository.Worktree.Commit(message, &git.CommitOptions{
			All: true,
		})
		if err != nil {
			log.Printf("failed to commit for %s: %v", repo, err)
		}

		err = repository.LocalRepo.Push(&git.PushOptions{
			RemoteName: "origin",
			Auth: &http.BasicAuth{
				Username: *user,
				Password: *pass,
			},
		})
		if err != nil {
			log.Printf("failed to push changes: %v", err)
		}

		createPR := &github.NewPullRequest{
			Title: github.String(message),
			Head:  github.String(string(branchName)),
			Base:  github.String("main"),
		}

		_, _, err = client.PullRequests.Create(context.Background(), "ministryofjustice", repo, createPR)
		if err != nil {
			log.Println(err)
		}

		break
	}

	// parse kubernetes.tpl file and marshal into a struct

	// change api version and add http path value

	// perform pull request against repo

}

func SplitYAML(resources []byte) ([][]byte, error) {
	dec := yaml.NewDecoder(bytes.NewReader(resources))

	var res [][]byte
	for {
		var value interface{}
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
