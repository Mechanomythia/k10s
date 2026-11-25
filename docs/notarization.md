# Signing and notarizing K10s.app in CI

Without this, CI ad-hoc signs the app and macOS quarantines downloads ("can't be opened", `-47`, `xattr -cr` needed). With it, downloads open normally.

The workflow (`.github/workflows/build.yml`, job `macos`) already contains the steps. They run as soon as the secrets below exist, and are skipped otherwise.

## 1. Apple Developer Program

Enroll at <https://developer.apple.com/programs/> (99 USD/year). An individual account is fine. Wait for the enrollment to be approved (minutes to a couple of days).

## 2. Create a "Developer ID Application" certificate

On your Mac:

1. Open **Keychain Access** → menu **Keychain Access → Certificate Assistant → Request a Certificate From a Certificate Authority…**. Enter your email, choose **Saved to disk**, and save the `.certSigningRequest`.
2. Go to <https://developer.apple.com/account/resources/certificates/add>, choose **Developer ID Application** (not "Apple Development", not "Mac App Distribution"), pick the **G2 Sub-CA** profile, and upload the request.
3. Download the `.cer` and double-click it. It lands in your login keychain, paired with the private key from step 1.
4. Check it:

```bash
security find-identity -v -p codesigning
```

You should see `Developer ID Application: Your Name (TEAMID1234)`.

## 3. Export it as a .p12 for CI

1. In Keychain Access → **My Certificates**, expand the Developer ID Application certificate so you see the private key under it.
2. Select **both** the certificate and the key → right-click → **Export 2 items…** → format **Personal Information Exchange (.p12)** → set a strong password.
3. Base64 it onto your clipboard:

```bash
base64 -i ~/Desktop/Certificates.p12 | pbcopy
```

Delete the `.p12` file afterwards; your keychain keeps the original.

## 4. Create an App Store Connect API key (for notarytool)

1. Open <https://appstoreconnect.apple.com/access/integrations/api> → **Team Keys** → **Generate API Key**. (If you see "Request Access", the Account Holder must click it once.)
2. Name it e.g. `k10s-notary` with role **Developer**.
3. Download `AuthKey_XXXXXXXXXX.p8`. You can download it only once.
4. Note the **Key ID** (shown next to the key) and the **Issuer ID** (at the top of the page).

An API key is preferred over an Apple ID plus app-specific password: it's not tied to your personal login and can be revoked on its own.

## 5. Add the GitHub secrets

Repository → **Settings → Secrets and variables → Actions → New repository secret**:

| Secret | Value |
|---|---|
| `MACOS_CERTIFICATE_P12` | the base64 text from step 3 |
| `MACOS_CERTIFICATE_PASSWORD` | the .p12 password |
| `NOTARY_KEY_P8` | the full contents of `AuthKey_XXXXXXXXXX.p8`, including the `-----BEGIN PRIVATE KEY-----` lines |
| `NOTARY_KEY_ID` | Key ID from step 4 |
| `NOTARY_ISSUER_ID` | Issuer ID from step 4 |

To paste the key file:

```bash
pbcopy < ~/Downloads/AuthKey_XXXXXXXXXX.p8
```

Then delete the downloaded `.p8`, or keep it in your password manager.

## 6. Run it

Push any commit to build, or publish a release: Actions → build → Run workflow, with `release_version` set (for example `0.1.0`). Releases are made only this way; pushing a tag does nothing.

In the `macOS app` job you should see:

- **Import Developer ID certificate** → `Signing as: Developer ID Application: …`
- **Build K10s.app**: every binary signed with `--options runtime --timestamp` (the hardened runtime notarization requires)
- **Notarize and staple**: `status: Accepted`, then `The staple and validate action worked!` and `spctl`: `accepted, source=Notarized Developer ID`

A rejection prints Apple's log with the exact file and reason.

## 7. Verify a download

```bash
spctl --assess --type execute -vv /Applications/K10s.app
```

Expected: `accepted` and `source=Notarized Developer ID`.

```bash
xcrun stapler validate /Applications/K10s.app
```

## Notes

- **What gets signed:** Sparkle's helpers and framework, `k10s-engine`, then the app, each with the hardened runtime. K10s needs no entitlements.
- **Stapling:** a zip can't be stapled, so CI staples the `.app` and re-zips it. The stapled ticket lets the app open offline on first launch.
- **Certificate expiry:** Developer ID certificates last 5 years. Apps signed and timestamped before expiry keep working; you only need a new `.p12` to sign new builds.
- **Forks and pull requests from forks** don't get secrets, so those builds stay ad-hoc. That's expected.
- **Rotation:** revoke the API key in App Store Connect and the certificate in the developer portal if a secret leaks, then replace the GitHub secrets.
