# Prototype Ingress Patcher

[![repo standards badge](https://img.shields.io/badge/dynamic/json?color=blue&style=for-the-badge&logo=github&label=MoJ%20Compliant&query=%24.result&url=https%3A%2F%2Foperations-engineering-reports.cloud-platform.service.justice.gov.uk%2Fapi%2Fv1%2Fcompliant_public_repositories%2Ftemplate-repository)](https://operations-engineering-reports.cloud-platform.service.justice.gov.uk/public-github-repositories.html#template-repository "Link to report")

This repository contains the Go code that performs the following:

- Accepts a list of ministryofjustice repositories.
- For each repository, it checks if the repository has an `kubernetes-deploy` resource (as defined in the prototype creation).
- If so, it will patch the `ingress` resource to add the appropriate annotations ready for the new ingress API.
- If changes are made, it will create a pull request with the changes.

## Usage

The prototype ingress patcher is a Go application that can be run locally or in a container.

### Running locally

To run the application locally, you will need to have Go installed on your machine. You can find instructions on how to do this [here](https://golang.org/doc/install).

Once Go is installed, you can run the application by running the following command:

```bash
go run patch.go -u <github-username> -p <github-person-access-token>
```

If you want to change the list of repositories that the application will run against, you can do so by editing the `repositories` variable in the `patch.go` file.
