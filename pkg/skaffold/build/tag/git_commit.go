/*
Copyright 2018 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	"github.com/pkg/errors"
	git "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

// GitCommit tags an image by the git commit it was built at.
type GitCommit struct {
}

// GenerateFullyQualifiedImageName tags an image with the supplied image name and the git commit.
func (c *GitCommit) GenerateFullyQualifiedImageName(workingDir string, opts *Options) (string, error) {
	repo, err := git.PlainOpenWithOptions(workingDir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return "", errors.Wrap(err, "opening git repo")
	}

	w, err := repo.Worktree()
	if err != nil {
		return "", errors.Wrap(err, "reading worktree")
	}

	status, err := w.Status()
	if err != nil {
		return "", errors.Wrap(err, "reading status")
	}

	head, err := repo.Head()
	if err != nil {
		return "", errors.Wrap(err, "determining current git commit")
	}

	commitHash := head.Hash().String()
	currentTag := commitHash[0:7]

	if status.IsClean() {
		tagrefs, _ := repo.Tags()
		err = tagrefs.ForEach(func(t *plumbing.Reference) error {
			if t.Hash() == head.Hash() {
				currentTag = t.Name().Short()
			}
			return nil
		})
		if err != nil {
			return "", errors.Wrap(err, "determining git tag")
		}

		fqn := fmt.Sprintf("%s:%s", opts.ImageName, currentTag)
		return fqn, nil
	}

	// The file state is dirty. To generate a unique suffix, let's hash all the modified files.
	// We add a -dirty-unique-id suffix to work well with local iterations.
	h := sha256.New()
	for _, changedPath := range changedPaths(status) {
		status := status[changedPath].Worktree

		statusLine := fmt.Sprintf("%c %s", status, changedPath)
		if _, err := h.Write([]byte(statusLine)); err != nil {
			return "", errors.Wrap(err, "adding deleted file to diff")
		}

		if status == git.Deleted {
			continue
		}

		f, err := w.Filesystem.Open(changedPath)
		if err != nil {
			return "", errors.Wrap(err, "reading diff")
		}

		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", errors.Wrap(err, "reading diff")
		}

		f.Close()
	}

	sha := h.Sum(nil)
	shaStr := hex.EncodeToString(sha[:])[:16]
	fqn := fmt.Sprintf("%s:%s-dirty-%s", opts.ImageName, currentTag, shaStr)
	return fqn, nil
}

// changedPaths returns the changed paths in a consistent order.
// The order is important because we generate a sha256 out of it.
func changedPaths(status git.Status) []string {
	var changes []string

	for path, change := range status {
		if change.Worktree != git.Unmodified {
			changes = append(changes, path)
		}
	}

	sort.Strings(changes)
	return changes
}
