# HEC release maintenance

## Keep release management ordinary

Use the checked-in maintenance scripts as ordinary host commands:

- `scripts/build.sh` builds one versioned HEC binary with the pinned Go toolchain at `/opt/hec/toolchains/go/1.26.2`.
- `scripts/install.sh` creates or verifies one complete immutable release at `/opt/hec/releases/<version>`.
- `scripts/cutover.sh <version>` explicitly selects an already installed release.

Installing a release does not change an existing `/opt/hec/current` link and does not restart the running service. Cutover validates the complete target, atomically replaces `current`, and restarts only `hec.service`.

There is no automatic rollback, fallback pointer, release daemon, updater, timer, watcher, controller, or release database. A restart failure leaves `current` pointing to the selected release and must be diagnosed directly. Selecting an older installed release is a manual release choice, not automatic rollback.

## Build a release binary

Run from a clean committed source tree and supply the version explicitly:

```bash
HEC_VERSION=0.0.11 ./scripts/build.sh
```

The script embeds the exact full source commit and a deterministic build date derived from the commit timestamp unless `SOURCE_DATE_EPOCH` is explicitly supplied. It builds through a temporary sibling and atomically replaces only the requested `HEC_OUTPUT` after compilation succeeds.

## Install an immutable release

Install with the same explicit version:

```bash
HEC_VERSION=0.0.11 ./scripts/install.sh
```

The installer stages under `/opt/hec/releases`, uses `build.sh`, packages the binary, Skills, capability manifests, and forge files, verifies version, protocol, and full commit, then atomically creates the release directory. Reinstalling identical content is accepted; differing content under the same version fails without overwrite, merge, or deletion.

When `/opt/hec/current` already exists, the installer preserves it and does not restart HEC. Inspect the inactive release before activation.

## Publish the exact commit

Use the owner-approved `HEC DEPLOY` key for native pushes without displaying or copying the private key. Bypass the system Git SSH-to-HTTPS rewrite only for the individual command:

```bash
GIT_CONFIG_NOSYSTEM=1 \
GIT_SSH_COMMAND='/usr/bin/ssh -i /root/.ssh/hec-stealtheye-hec-deploy-ed25519 -o IdentitiesOnly=yes -o BatchMode=yes' \
git push git@github.com:StealthEyeLLC/hec.git HEAD:refs/heads/main
```

Use the same command-scoped environment for an exact fetch into `origin/main`. Do not change `/etc/gitconfig`, do not permanently change `origin`, and do not use alternate credentials while the designated GitHub App and deploy key are available. Independently verify the final remote commit through the designated GitHub App connector.

## Activate through a durable HEC job

An HEC-initiated cutover must use `job.start`, because restarting `hec.service` can interrupt a synchronous `run` response:

```text
sleep 2
/work/hec/scripts/cutover.sh 0.0.11
```

Use a unique idempotency key, retain the returned job handle, and allow the request to return before the restart. The tmux-backed terminal state survives the HEC restart.

After reconnecting through the same HEC app, verify:

- `health` succeeds;
- `version` reports the intended version, protocol, full commit, and deterministic date;
- `/opt/hec/current` resolves to the selected immutable release;
- `hec.service` is active, enabled, root-owned, and still uses `RuntimeDirectoryPreserve=yes`;
- local `main`, `origin/main`, GitHub App remote `main`, and installed build metadata all report the same full commit.

Then wait for the durable cutover job, read its output, and forget only that dedicated job.

## Normal release flow

1. Commit clean source.
2. Run Go, shell, forge, Skill, and regression tests.
3. Install the new release while leaving the existing `current` active.
4. Inspect the installed release and confirm its metadata.
5. Push the exact commit with `HEC DEPLOY` and verify it through the designated GitHub App.
6. Start `scripts/cutover.sh <version>` through `job.start`.
7. Reconnect through the same HEC app without refreshing or republishing it.
8. Verify health, version, service state, release target, terminal continuity, and Git equality.

## Manually select an older installed release

To select an older valid immutable release, start another dedicated durable job that runs:

```bash
/work/hec/scripts/cutover.sh 0.0.10
```

Reconnect and verify the selected version and `current` target. Select the newer release again with another explicit durable cutover when the test is complete. This is manual release selection, not automatic rollback, and it creates no previous-release pointer.
