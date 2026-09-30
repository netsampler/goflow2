# Security policy

## Reporting a vulnerability

Please report suspected security vulnerabilities privately. Do not include
vulnerability details, exploit code, or sensitive packet captures in public
issues or pull requests.

If GitHub private vulnerability reporting is enabled, use **Report a vulnerability**
on the [Security advisories page](https://github.com/netsampler/goflow2/security/advisories).
GitHub keeps these reports private between the reporter and project maintainers.

If that button is unavailable, open a
[public issue](https://github.com/netsampler/goflow2/issues/new)
titled **Security contact request** asking maintainers how to contact them
privately. Include no vulnerability details in that issue; wait for a private
reporting channel before sharing them.

For ordinary bugs and feature requests, use the
[issue tracker](https://github.com/netsampler/goflow2/issues).

## What to include in a private report

- The affected GoFlow2 version or commit, operating system, and relevant configuration.
- A description of the vulnerability, its potential impact, and any conditions
  required to trigger it.
- Minimal reproduction steps or a proof of concept, including a sample packet
  or packet capture if relevant.
- Any suggested mitigation or fix.

Remove credentials, personal information, and unrelated network traffic from
logs, configurations, and packet captures before sharing them.

Potential security issues include remotely triggered crashes, excessive memory
or CPU consumption while decoding flow packets, and unintended exposure of
sensitive information. If you are unsure whether a bug has security implications,
use the private reporting process above.

## Versions and disclosure

Reports about any version are welcome. Include whether the issue also affects
the [latest stable release](https://github.com/netsampler/goflow2/releases/latest)
if you can check it. Reporting an issue in an older release does not imply that
a fix will be backported to that release.

Please coordinate public disclosure with the maintainers so that a fix or
mitigation can be made available to users. Response and remediation times depend
on maintainer availability and the issue's complexity.
