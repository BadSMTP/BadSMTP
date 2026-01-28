Packaging notes - UFW application profile for BadSMTP

This file documents how to include the UFW application profile in the Debian package.

Files:
- debian/ufw/badsmtp.profile  -> installed to /usr/share/ufw/applications.d/badsmtp
- debian/badsmtp.install      -> entry to include the above profile into package payload
- debian/postinst             -> maintainer script snippet that registers the profile with ufw

Steps for packagers
1. Ensure `debian/ufw/badsmtp.profile` is present (this repository includes it).
2. Ensure `debian/badsmtp.install` contains the line:

   debian/ufw/badsmtp.profile usr/share/ufw/applications.d/

   This will copy the profile into the package payload at build time.

3. Include `debian/postinst` in the package. The `postinst` in this repository will:
   - create the system user and mailbox dirs (existing behaviour)
   - if UFW is installed and active, run `ufw app update badsmtp` to register the profile
   - optionally run `ufw --force limit badsmtp` if the installer sets BADSMTP_UFW_LIMIT=1

   The postinst is intentionally non-fatal when `ufw` is absent so package installations on systems
   without UFW do not fail.

Notes about `limit` and rate-limiting
- UFW application profiles cannot encode the `limit` action. `limit` is an operational rule and
  must be applied using `ufw limit <app|port>`.
- To enable rate-limiting at package install time set the environment variable `BADSMTP_UFW_LIMIT=1`.
  For example:

    BADSMTP_UFW_LIMIT=1 dpkg -i badsmtp-amd64.deb

- Alternatively, administrators can run:

    ufw --force limit badsmtp

  after installation to enable per-source connection throttling for the BadSMTP profile.

Security and policy
- The package does not enable or alter firewall rules by default. The optional rate-limited
  rule is available only if the packager/administrator explicitly requests it (via the env var
  or by running `ufw` manually).

Support
- If you want a different default behavior for firewalling (e.g., open only 2525/TLS and not the
  delay ports), modify `debian/ufw/badsmtp.profile` and `debian/postinst` accordingly before
  building the package.
