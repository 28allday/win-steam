# Code signing policy

This program uses free code signing provided by [SignPath.io](https://signpath.io),
with a certificate by the [SignPath Foundation](https://signpath.org).

*(Application pending — releases are unsigned until it is approved. The
SHA-256 of each release exe is listed in its release notes for manual
verification in the meantime.)*

## Integrity

Signed binaries are built from the source code in this repository
(<https://github.com/28allday/win-steam>) via the release build described
in `build.sh`. Nothing is added that is not in the repository; the only
third-party code embedded is the upstream
[steamos-nvidia-installer](https://github.com/28allday/steamos-nvidia-installer)
script (same author, MIT) as documented in the README.

## Team roles

This is a single-maintainer project. [Gavin Nugent (@28allday)](https://github.com/28allday)
acts as author, reviewer, and approver; every signed release is manually
approved by the maintainer. Two-factor authentication is enabled on the
GitHub account and on the SignPath account.

## Privacy

This program does not transfer any user data to the maintainer or to any
third party. It contacts remote servers only for its documented function,
at the user's direction: Valve's official SteamOS recovery-image page,
Microsoft's WSL distribution endpoints, and Arch Linux package
mirrors/archives. No telemetry, no analytics, no accounts.

## Uninstalling

The app is a single portable exe — delete it to remove it. The WSL builder
distro it creates can be removed from within the app (sidebar → remove
builder) or with `wsl --unregister steamos-builder`.
