#!/bin/bash

# go get github.com/buchanae/github-release-notes

cat <<EOF
# :sparkles: What's new
$(github-release-notes -org vmware-tanzu -repo carvel-imgpkg -since-latest-release)

# :bug: Bug Fixes

# :speaker: Callouts
Thanks to
- User 1 @bananas

For helping out with this release
done
EOF

