// Runs semantic-release for real (no dryRun) -- creates the actual git tag
// and GitHub Release -- and writes `released=true|false`, and if true,
// `version`/`major`/`minor`, directly to $GITHUB_OUTPUT.
//
// Unlike the sibling sonarqube-compose repo (which has no build artifact
// of its own and just runs the plain CLI), this repo's `publish` job needs
// to know the exact version to tag the Docker image with, and whether a
// release actually happened at all (skip publishing image version tags
// otherwise) -- the programmatic API's return value gives both directly,
// which the CLI's plain exit code/log output doesn't expose cleanly.
//
// See print-next-version.mjs for why $GITHUB_OUTPUT is written via fs
// here rather than shell-redirecting this script's stdout.
import { appendFileSync } from "node:fs";
import semanticRelease from "semantic-release";

const result = await semanticRelease();

if (result) {
  const { version } = result.nextRelease;
  const [major, minor] = version.split(".");
  appendFileSync(
    process.env.GITHUB_OUTPUT,
    `released=true\nversion=${version}\nmajor=${major}\nminor=${minor}\n`,
  );
  console.log(`Released ${version}`);
} else {
  appendFileSync(process.env.GITHUB_OUTPUT, "released=false\n");
  console.log("No release published (nothing warranted one)");
}
