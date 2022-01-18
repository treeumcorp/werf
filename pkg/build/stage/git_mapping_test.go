package stage_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

/*
image: jekyll_base
fromCacheVersion: "1"
from: jekyll/builder:4
git:
  - add: /
    to: /app
    owner: jekyll
    group: jekyll
    includePaths:
      - Gemfile
      - Gemfile.lock
    stageDependencies:
      setup: ["**/*"]
shell:
setup:
- cd /app
- bundle install
*/

/*
│ ┌ Building stage jekyll_base/gitLatestPatch
│ │ jekyll_base/gitLatestPatch  /.werf/stapel/embedded/bin/bash: warning: setlocale: LC_ALL: cannot change locale (en_US.UTF-8)
│ │ jekyll_base/gitLatestPatch  /.werf/stapel/embedded/bin/bash: warning: setlocale: LC_ALL: cannot change locale (en_US.UTF-8)
│ │ jekyll_base/gitLatestPatch  error: patch failed: /app/Gemfile.lock:18
│ │ jekyll_base/gitLatestPatch  error: /app/Gemfile.lock: patch does not apply
│ ├ Info
│ └ Building stage jekyll_base/gitLatestPatch (1.02 seconds) FAILED
└ ⛵ image jekyll_base (2.69 seconds) FAILED
*/

var _ = Describe("Git mapping", func() {
	Context("when working with virtual merge commit", func() {
		Context("when previously built stage related to virtual merge commit and current commit does not contain changes")
		AfterEach(func() {
			//
		})

		It("should report Deployment is ready before werf exit", func() {

		})
	})
t})
