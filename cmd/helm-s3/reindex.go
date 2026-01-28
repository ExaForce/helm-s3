package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/hypnoglow/helm-s3/internal/awss3"
	"github.com/hypnoglow/helm-s3/internal/awsutil"
	"github.com/hypnoglow/helm-s3/internal/helmutil"
	log "github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/repo"
)

const reindexDesc = `This command performs a reindex of the repository.

'helm s3 push' takes one argument:
- REPO - target repository.
`

const reindexExample = `  helm s3 reindex my-repo - performs a reindex of the repository with name 'my-repo'.`
const batchSize = 1000

func newReindexCommand(opts *options) *cobra.Command {
	act := &reindexAction{
		printer:  nil,
		acl:      "",
		verbose:  false,
		repoName: "",
		relative: false,
		dryRun:   false,
	}

	cmd := &cobra.Command{
		Use:     "reindex REPO",
		Short:   "Reindex the repository.",
		Long:    reindexDesc,
		Example: reindexExample,
		Args:    wrapPositionalArgsBadUsage(cobra.ExactArgs(1)),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// No completions for the REPO argument.
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			act.printer = cmd
			act.acl = opts.acl
			act.verbose = opts.verbose
			act.repoName = args[0]
			return act.run(cmd.Context())
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&act.relative, "relative", act.relative, "Use relative chart URLs in the index instead of absolute.")
	flags.BoolVar(&act.dryRun, "dry-run", act.dryRun, "Simulate reindex, don't push it to the dest repo.")

	return cmd
}

type reindexAction struct {
	printer printer

	// global flags

	acl     string
	verbose bool

	// args

	repoName string

	// flags

	relative bool

	dryRun bool
}

func (act *reindexAction) run(ctx context.Context) error {
	start := time.Now()

	repoEntry, err := helmutil.LookupRepoEntry(act.repoName)
	if err != nil {
		return err
	}

	sess, err := awsutil.Session(awsutil.DynamicBucketRegion(repoEntry.URL()))
	if err != nil {
		return err
	}
	storage := awss3.New(sess)

	// Step 1: Fetch current index from S3
	log.Info("fetching current index from S3")
	currentIndex := repo.NewIndexFile()
	indexURI := helmutil.IndexFileURL(repoEntry.URL())
	indexData, err := storage.FetchRaw(ctx, indexURI)
	if err != nil {
		if err == awss3.ErrObjectNotFound {
			log.Info("no existing index found, will create new one")
		} else {
			return errors.Wrap(err, "fetch current index")
		}
	} else {
		if err := yaml.Unmarshal(indexData, currentIndex); err != nil {
			return errors.Wrap(err, "unmarshal current index")
		}
		log.Infof("loaded existing index with %d chart entries", len(currentIndex.Entries))
	}

	// Step 2: List all chart files from S3 (no HEAD requests)
	log.Info("listing chart files from S3")
	s3Files, err := storage.ListChartFiles(ctx, repoEntry.URL())
	if err != nil {
		return errors.Wrap(err, "list chart files")
	}

	// Build set of S3 files for quick lookup
	s3FileSet := make(map[string]bool, len(s3Files))
	for _, f := range s3Files {
		s3FileSet[f] = true
	}

	// Step 3: Build set of files currently in index
	indexFileSet := make(map[string]bool)
	for _, versions := range currentIndex.Entries {
		for _, cv := range versions {
			for _, url := range cv.URLs {
				// Extract filename from URL
				filename := url
				if idx := len(url) - 1; idx >= 0 {
					for i := len(url) - 1; i >= 0; i-- {
						if url[i] == '/' {
							filename = url[i+1:]
							break
						}
					}
				}
				indexFileSet[filename] = true
			}
		}
	}

	// Step 4: Find new charts (in S3 but not in index)
	var newCharts []string
	for _, f := range s3Files {
		if !indexFileSet[f] {
			newCharts = append(newCharts, f)
		}
	}
	log.Infof("found %d new charts to add", len(newCharts))

	// Step 5: Find deleted charts (in index but not in S3)
	var deletedCharts []string
	for filename := range indexFileSet {
		if !s3FileSet[filename] {
			deletedCharts = append(deletedCharts, filename)
		}
	}
	log.Infof("found %d charts to remove (no longer in S3)", len(deletedCharts))

	// Step 6: Remove deleted charts from index
	for _, filename := range deletedCharts {
		// Find and remove the entry
		for name, versions := range currentIndex.Entries {
			var remaining []*repo.ChartVersion
			for _, cv := range versions {
				keep := true
				for _, url := range cv.URLs {
					if len(url) >= len(filename) && url[len(url)-len(filename):] == filename {
						keep = false
						break
					}
				}
				if keep {
					remaining = append(remaining, cv)
				}
			}
			if len(remaining) == 0 {
				delete(currentIndex.Entries, name)
			} else {
				currentIndex.Entries[name] = remaining
			}
		}
	}

	// Step 7: Fetch metadata for new charts only (HEAD requests)
	for _, filename := range newCharts {
		if act.verbose {
			act.printer.Printf("[DEBUG] Fetching metadata for %s\n", filename)
		}

		chartInfo, err := storage.GetChartInfo(ctx, repoEntry.URL(), filename)
		if err != nil {
			log.Warnf("failed to get chart info for %s: %s", filename, err)
			continue
		}

		baseURL := repoEntry.URL()
		if act.relative {
			baseURL = ""
		}

		escapedFilename := escapeIfRelative(chartInfo.Filename, act.relative)
		if err := currentIndex.MustAdd(chartInfo.Meta.Value().(*chart.Metadata), escapedFilename, baseURL, chartInfo.Hash); err != nil {
			act.printer.PrintErrf("[ERROR] failed to add chart to the index: %s", err)
		}
	}

	currentIndex.SortEntries()

	// Step 8: Write and upload index
	if err := currentIndex.WriteFile(repoEntry.CacheFile(), helmutil.DefaultIndexFilePerm); err != nil {
		return errors.WithMessage(err, "update local index")
	}

	file, err := os.Open(repoEntry.CacheFile())
	if err != nil {
		return errors.Wrap(err, "open index file")
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return errors.Wrap(err, "get file stats")
	}

	ra := make([]byte, stat.Size())
	if _, err := bufio.NewReader(file).Read(ra); err != nil && err != io.EOF {
		return errors.Wrap(err, "read index file")
	}

	r := bytes.NewReader(ra)

	if !act.dryRun {
		if err := storage.PutIndex(ctx, repoEntry.URL(), act.acl, r); err != nil {
			return errors.Wrap(err, "upload index to the repository")
		}
	} else {
		act.printer.Printf("[DEBUG] Dry run, not pushing index to the repository.\n")
	}

	totalCharts := 0
	for _, versions := range currentIndex.Entries {
		totalCharts += len(versions)
	}

	act.printer.Printf("Repository %s was successfully reindexed.\n", act.repoName)
	log.Infof("reindex completed: %d total charts (%d added, %d removed) in %s",
		totalCharts, len(newCharts), len(deletedCharts), time.Since(start))
	return nil
}
