package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"gopkg.in/yaml.v3"
)

type manifest struct {
	Ing ingress
	Dep deployment
	Svc service
}

type template struct {
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
					Path    string `yaml:"path"`
					Backend struct {
						ServiceName string `yaml:"serviceName"`
						ServicePort int    `yaml:"servicePort"`
					} `yaml:"backend"`
				} `yaml:"paths"`
			} `yaml:"http"`
		} `yaml:"rules"`
	} `yaml:"spec"`
}

func main() {
	const (
		base   = "https://github.com/ministryofjustice/"
		newApi = "networking.k8s.io/v1"
		oldApi = "networking.k8s.io/v1beta1"
	)

	var (
		user = flag.String("u", "", "github username")
		pass = flag.String("p", "", "github password")
	)

	flag.Parse()

	// define a slice of repositories to patch
	repos := []string{
		"apply-for-compensation-prototype",
		"apply-for-legal-aid-prototype",
		"book-a-prison-visit-prototype",
		"cloud-platform-prototype-demo",
		"dex-ia-proto",
		"eligibility-estimate",
		"hmpps-assess-risks-and-needs-prototypes",
		"hmpps-incentives-tool",
		"hmpps-interventions-prototype",
		"hmpps-licenses-prototype",
		"hmpps-manage-supervisions-prototype",
		"hmpps-prepare-a-case-prototype",
		"hmpps-prisoner-education",
		"interventions-design-history",
		"jason-design-demo",
		"laa-crime-apply-prototype",
		"laa-view-court-data-prototype",
		"makerecall-prototype",
		"manage-supervisions-design-history",
		"opg-lpa-fd-prototype",
		"opg-sirius-prototypes",
		"request-info-from-moj",
		"send-legal-mail-prototype",
	}

	m := manifest{}

	// loop through repositories and git clone
	err := os.Mkdir("./tmp/", 0755)
	if err != nil {
		log.Println(err)
	}

	err = os.Chdir("./tmp/")
	if err != nil {
		log.Println(err)
	}

	for _, repo := range repos {
		fmt.Println("cloning " + repo)
		localRepo, err := git.PlainClone(repo, false, &git.CloneOptions{
			URL: base + repo,
			Auth: &http.BasicAuth{
				Username: *user,
				Password: *pass,
			},
		})
		if err != nil {
			log.Printf("failed to clone %s: %v", repo, err)
			continue
		}

		// Get HEAD ref from repository
		ref, err := localRepo.Head()
		if err != nil {
			log.Printf("failed to get HEAD ref for %s: %v", repo, err)
		}

		// Get the worktree for the local repository
		tree, err := localRepo.Worktree()
		if err != nil {
			log.Printf("failed to get worktree for %s: %v", repo, err)
		}
		branchName := plumbing.NewBranchReferenceName("ingress-patch")

		create := &git.CheckoutOptions{
			Hash:   ref.Hash(),
			Branch: branchName,
			Create: true,
		}

		err = tree.Checkout(create)
		if err != nil {
			log.Printf("failed to checkout branch for %s: %v", repo, err)
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
			var t template
			err := yaml.Unmarshal(byteSlice, &t)
			if err != nil {
				log.Printf("failed to unmarshal yaml for %s: %v", repo, err)
			}
			fmt.Printf("Kind is %s\n", t.Kind)
			// case switch to create a deployment, service and ingress
			if t.Kind == "Deployment" {
				fmt.Println("found deployment")
				err = yaml.Unmarshal(byteSlice, &m.Dep)
				if err != nil {
					log.Printf("failed to unmarshal yaml for %s: %v", repo, err)
				}
			}

			if t.Kind == "Service" {
				fmt.Println("found service", t.Kind)
				err = yaml.Unmarshal(byteSlice, &m.Svc)
				if err != nil {
					log.Printf("failed to unmarshal yaml for %s: %v", repo, err)
				}
			}

			if t.Kind == "Ingress" {
				fmt.Println("found ingress")
				err = yaml.Unmarshal(byteSlice, &m.Ing)
				if err != nil {
					log.Printf("failed to unmarshal yaml for %s: %v", repo, err)
				}
			}
		}
		fmt.Println("here's the manifest", m)
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
