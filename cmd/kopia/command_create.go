package main

import (
	"crypto/rand"
	"fmt"
	"io"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/storage"
	"github.com/kopia/kopia/vault"
)

var (
	createCommand           = app.Command("create", "Create new vault and repository.")
	createCommandRepository = createCommand.Flag("repository", "Repository path.").Default("colocated").String()
	createObjectFormat      = createCommand.Flag("repo-format", "Format of repository objects.").PlaceHolder("FORMAT").Default("sha256-fold160-aes128").Enum(supportedObjectFormats()...)

	createMaxBlobSize           = createCommand.Flag("max-blob-size", "Maximum size of a data chunk.").PlaceHolder("KB").Default("20480").Int()
	createInlineBlobSize        = createCommand.Flag("inline-blob-size", "Maximum size of an inline data chunk.").PlaceHolder("KB").Default("32").Int()
	createVaultEncryptionFormat = createCommand.Flag("vault-encryption", "Vault encryption.").PlaceHolder("FORMAT").Default("aes-256").Enum(supportedVaultEncryptionFormats()...)
	createOverwrite             = createCommand.Flag("overwrite", "Overwrite existing data (DANGEROUS).").Bool()
	createOnly                  = createCommand.Flag("create-only", "Create the vault, but don't connect to it.").Short('c').Bool()
)

func init() {
	createCommand.Action(runCreateCommand)
}

func vaultFormat() (*vault.Format, error) {
	f := &vault.Format{
		Version:  "1",
		Checksum: "hmac-sha-256",
	}
	f.UniqueID = make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, f.UniqueID)
	if err != nil {
		return nil, err
	}
	f.Encryption = *createVaultEncryptionFormat
	return f, nil
}

func repositoryFormat() (*repo.Format, error) {
	f := &repo.Format{
		Version:           "1",
		Secret:            make([]byte, 32),
		MaxBlobSize:       *createMaxBlobSize * 1024,
		MaxInlineBlobSize: *createInlineBlobSize * 1024,
		ObjectFormat:      *createObjectFormat,
	}

	_, err := io.ReadFull(rand.Reader, f.Secret)
	if err != nil {
		return nil, err
	}

	return f, nil
}

func openStorageAndEnsureEmpty(url string) (storage.Storage, error) {
	s, err := newStorageFromURL(url)
	if err != nil {
		return nil, err
	}
	ch := s.ListBlocks("")
	_, hasData := <-ch

	if hasData && !*createOverwrite {
		return nil, fmt.Errorf("found existing data in %v, specify --overwrite to use anyway", url)
	}

	return s, nil
}

func runCreateCommand(context *kingpin.ParseContext) error {
	if *vaultPath == "" {
		return fmt.Errorf("--vault is required")
	}
	vaultStorage, err := openStorageAndEnsureEmpty(*vaultPath)
	if err != nil {
		return fmt.Errorf("unable to get vault storage: %v", err)
	}

	var repositoryStorage storage.Storage

	if *createCommandRepository == "colocated" {
		repositoryStorage = vaultStorage
	} else {
		repositoryStorage, err = openStorageAndEnsureEmpty(*createCommandRepository)
		if err != nil {
			return fmt.Errorf("unable to get repository storage: %v", err)
		}
	}

	repoFormat, err := repositoryFormat()
	if err != nil {
		return fmt.Errorf("unable to initialize repository format: %v", err)
	}

	fmt.Printf(
		"Initializing repository in with format '%v' and maximum object size %v.\n",
		repoFormat.ObjectFormat,
		repoFormat.MaxBlobSize)

	vf, err := vaultFormat()
	if err != nil {
		return fmt.Errorf("unable to initialize vault format: %v", err)
	}

	creds, err := getVaultCredentials(true)
	if err != nil {
		return fmt.Errorf("unable to get credentials: %v", err)
	}

	// Make repository to make sure the format is supported.
	_, err = repo.NewRepository(repositoryStorage, repoFormat)
	if err != nil {
		return fmt.Errorf("unable to initialize repository: %v", err)
	}

	fmt.Printf(
		"Initializing vault with encryption '%v'.\n",
		vf.Encryption)
	vlt, err := vault.Create(vaultStorage, vf, creds, repositoryStorage, repoFormat)
	if err != nil {
		return fmt.Errorf("cannot create vault: %v", err)
	}

	if !*createOnly {
		if err := persistVaultConfig(vlt); err != nil {
			return err
		}

		fmt.Println("Connected to vault:", *vaultPath)
	}

	return nil
}

func supportedVaultEncryptionFormats() []string {
	return []string{
		"none",
		"aes-128",
		"aes-192",
		"aes-256",
	}
}

func supportedObjectFormats() []string {
	var r []string
	for _, o := range repo.SupportedFormats {
		r = append(r, o.Name)
	}
	return r
}
