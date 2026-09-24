// Writes `version=X.Y.Z` directly to $GITHUB_OUTPUT -- the version this
// commit would become per .releaserc.json's rules, computed against
// commits since the last `vX.Y.Z` tag, WITHOUT creating a tag or GitHub
// Release (dryRun).
//
// Only ever invoked on `push` events -- ci.yml's `version` job doesn't
// even exist (job-level `if:`) on `pull_request` runs: semantic-release's
// branch-matching check reads GitHub's own GITHUB_REF env var directly
// and always refuses on a PR's detached synthetic merge ref, confirmed by
// testing in the sibling sonarqube-compose repo (neither `ci: false` nor a
// same-named local branch changes that). A `push` to `main` really is
// checked out on refs/heads/main, so this works here without any tricks.
// `sonarqube` supplies its own plain-git fallback for PR runs, where this
// script never runs at all.
//
// Deliberately writes to $GITHUB_OUTPUT via fs, not by shell-redirecting
// this script's stdout: semantic-release's own logger writes its progress
// lines (e.g. "[semantic-release] > i Running semantic-release version
// ...") directly to stdout regardless of what this script returns, and
// GitHub's runner tries to parse *every* line appended to $GITHUB_OUTPUT
// as a strict key=value pair -- those log lines aren't, so a naive
// `node script.mjs >> "$GITHUB_OUTPUT"` fails with "Invalid format".
//
// Falls back to the latest existing tag (stripped of its `v` prefix) if
// semantic-release determines no release is warranted for the current
// HEAD, so sonar.projectVersion always has *something* meaningful rather
// than an empty string.
import { execSync } from "node:child_process";
import { appendFileSync } from "node:fs";
import semanticRelease from "semantic-release";

const result = await semanticRelease({ dryRun: true });

const version = result
  ? result.nextRelease.version
  : execSync("git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0")
      .toString()
      .trim()
      .replace(/^v/, "");

appendFileSync(process.env.GITHUB_OUTPUT, `version=${version}\n`);
console.log(`Computed next version: ${version}`);
