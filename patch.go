package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"gopkg.in/yaml.v3"
)

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

	// loop through repositories and git clone
	err := os.Mkdir("./tmp/", 0755)
	if err != nil {
		log.Println(err)
	}

	os.Chdir("./tmp/")
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
		bytes, err := os.ReadFile(kFile)
		if err != nil {
			log.Printf("failed to read file for %s: %v", repo, err)
		}

		ing := ingress{}
		err = yaml.Unmarshal(bytes, &ing)
		if err != nil {
			log.Printf("failed to unmarshal yaml for %s: %v", repo, err)
		}

		fmt.Println(ing)
	}

	// parse kubernetes.tpl file and marshal into a struct

	// change api version and add http path value

	// perform pull request against repo

}
