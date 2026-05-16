## Summary

<!-- One or two sentences. What changed and why. -->

## Checklist

- [ ] Tests cover the new behaviour (or the absence is justified).
- [ ] `npm run lint` and `npm run typecheck` pass locally
      (root + `datasource/`).
- [ ] If this PR touches `provisioning/alerting/*.yaml`: the
      provisioning gate in CI passes and the change is reflected
      in the README's alert-rule documentation.
- [ ] No raw bearer / token references in any logger or
      `console.*` call (CI greps Go and TS surfaces).
- [ ] No new filesystem or arbitrary network access in
      `datasource/pkg/`; only the configured Echo endpoint via the
      SDK HTTP client.
- [ ] CHANGELOG entry added.

## How was this tested?

<!-- Manual steps, screenshots if UI-affecting, etc. -->
