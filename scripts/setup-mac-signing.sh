#!/usr/bin/env bash
# Upload the macOS code-signing secrets that release.yml's publish-desktop job
# reads. Run it once now, and again whenever the certificate is replaced —
# Developer ID certificates expire, and the failure mode when they do is a
# release that builds, warns, and quietly ships something Gatekeeper blocks.
#
#   scripts/setup-mac-signing.sh <DeveloperID.p12> [AuthKey_XXXXXXXX.p8]
#
# The .p12 is exported from Keychain Access: My Certificates -> right-click
# "Developer ID Application: ... (WTWBLR82TY)" -> Export. Exporting from there
# rather than with `security export` is deliberate: the CLI exports every
# identity in the keychain, and this secret should carry one.
#
# The .p8 is the App Store Connect API key used for notarization
# (https://appstoreconnect.apple.com/access/integrations/api, Developer role).
# It can only be downloaded once. Omit it to set up signing alone: the workflow
# warns and skips notarization rather than failing.
#
# Nothing here echoes a secret or writes one to disk. The password is read with
# no terminal echo and piped straight to `gh`.
set -euo pipefail

REPO="${OCTO_REPO:-juancavallotti/octo}"
TEAM_ID="WTWBLR82TY"

# %b so an embedded \n in a message renders as a newline.
die() { printf '\nerror: %b\n' "$1" >&2; exit 1; }

[ $# -ge 1 ] || die "usage: $0 <DeveloperID.p12> [AuthKey_XXXXXXXX.p8]"
P12="$1"
P8="${2:-}"

[ -r "$P12" ] || die "cannot read $P12"
# Checked here rather than where it is used, further down: uploading the
# certificate and only then discovering a typo in the second argument leaves the
# repository half-configured, which is how this script first behaved.
[ -z "$P8" ] || [ -r "$P8" ] || die "cannot read $P8"
command -v gh >/dev/null || die "the gh CLI is not installed"
gh auth status >/dev/null 2>&1 || die "gh is not authenticated; run: gh auth login"

printf 'Repository : %s\n' "$REPO"
printf 'Certificate: %s\n\n' "$P12"

# Read once, and verify it actually opens the file before anything is uploaded.
# A wrong password here would otherwise surface as a release failing twenty
# minutes into a macOS runner, with an error about the keychain rather than
# about the password.
printf 'Export password for the .p12 (not echoed): '
read -rs P12_PASS
printf '\n'
[ -n "$P12_PASS" ] || die "an empty export password will not import on the runner"

if ! openssl pkcs12 -in "$P12" -passin "pass:$P12_PASS" -nokeys -legacy >/dev/null 2>&1 \
  && ! openssl pkcs12 -in "$P12" -passin "pass:$P12_PASS" -nokeys >/dev/null 2>&1; then
  die "that password does not open $P12"
fi

# Confirm it is the right kind of certificate, and the right team. A .p12 holding
# an "Apple Development" cert imports and signs happily, and then fails
# notarization — which is a slow and confusing way to learn what went in.
SUBJECT="$(openssl pkcs12 -in "$P12" -passin "pass:$P12_PASS" -nokeys -legacy 2>/dev/null \
  || openssl pkcs12 -in "$P12" -passin "pass:$P12_PASS" -nokeys 2>/dev/null \
  | true)"
SUBJECT="$(printf '%s' "$SUBJECT" | openssl x509 -noout -subject 2>/dev/null || true)"
case "$SUBJECT" in
  *"Developer ID Application"*) : ;;
  "") printf 'warning: could not read the certificate subject; continuing.\n' >&2 ;;
  *) die "this is not a Developer ID Application certificate:\n  $SUBJECT" ;;
esac
case "$SUBJECT" in
  ""|*"$TEAM_ID"*) : ;;
  *) die "certificate is not for team $TEAM_ID, which release.yml pins:\n  $SUBJECT" ;;
esac

# -A keeps it on one line: CSC_LINK is read as a single base64 string, and the
# wrapped output macOS `base64` produces by default does not survive that.
openssl base64 -A -in "$P12" | gh secret set MAC_CSC_LINK --repo "$REPO"
printf '%s' "$P12_PASS" | gh secret set MAC_CSC_KEY_PASSWORD --repo "$REPO"
unset P12_PASS
printf '  set MAC_CSC_LINK, MAC_CSC_KEY_PASSWORD\n'

if [ -z "$P8" ]; then
  printf '\nNo App Store Connect key given, so notarization stays off: releases\n'
  printf 'will be signed but Gatekeeper still warns on download. Re-run with the\n'
  printf '.p8 path to finish.\n'
  exit 0
fi

printf '\nApp Store Connect Key ID (e.g. ABCD1234EF): '
read -r KEY_ID
printf 'App Store Connect Issuer ID (a UUID): '
read -r ISSUER

[ -n "$KEY_ID" ] || die "the Key ID is required"
case "$ISSUER" in
  [0-9a-fA-F]*-*-*-*-*) : ;;
  *) die "the Issuer ID should be a UUID; got: $ISSUER" ;;
esac

# Ask Apple whether these three actually authenticate, the same way the p12
# password is checked above. `history` is read-only and cheap, and a revoked or
# wrong-role key is otherwise indistinguishable from a working one until a
# release has already built and is trying to staple.
if [ "${SKIP_NOTARY_CHECK:-}" = "1" ]; then
  printf '  skipping the credential check (SKIP_NOTARY_CHECK=1)\n'
elif ! command -v xcrun >/dev/null || ! xcrun --find notarytool >/dev/null 2>&1; then
  printf 'warning: notarytool not found; uploading without verifying the key.\n' >&2
else
  printf '  checking the key against App Store Connect... '
  if OUT="$(xcrun notarytool history --key "$P8" --key-id "$KEY_ID" --issuer "$ISSUER" 2>&1)"; then
    printf 'ok\n'
  else
    printf 'failed\n'
    die "these credentials were refused:\n$OUT\n\nRe-run with SKIP_NOTARY_CHECK=1 if this is a network problem rather than a bad key."
  fi
fi

gh secret set APPLE_API_KEY_P8 --repo "$REPO" < "$P8"
printf '%s' "$KEY_ID" | gh secret set APPLE_API_KEY_ID --repo "$REPO"
printf '%s' "$ISSUER" | gh secret set APPLE_API_ISSUER --repo "$REPO"
printf '  set APPLE_API_KEY_P8, APPLE_API_KEY_ID, APPLE_API_ISSUER\n'

printf '\nDone. The next tagged release produces a signed, notarized build.\n'
printf 'Verify a downloaded DMG with:  spctl -a -vvv -t install /Applications/Octo.app\n'
