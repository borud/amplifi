#!/usr/bin/env bash
# Print the Homebrew formula for a release, using the tarball hashes from
# SHA256SUMS. BASE_URL overrides where the tarballs are downloaded from.
#
#   scripts/homebrew-formula.sh v1.2.0 dist/SHA256SUMS > Formula/amplifi.rb
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 <tag> <SHA256SUMS>" >&2
	exit 2
fi

tag=$1
sums=$2
base=${BASE_URL:-https://github.com/borud/amplifi/releases/download/${tag}}

# block prints the url and sha256 lines for one OS_ARCH platform. Every
# block is built before printing, as an exit inside $(...) would not stop
# the script.
block() {
	local file="amplifi_${tag}_$1.tar.gz"
	local hash
	hash=$(awk -v f="$file" '$2 == f { print $1 }' "$sums")
	if [[ -z $hash ]]; then
		echo "no checksum for $file in $sums" >&2
		return 1
	fi
	printf '      url "%s/%s"\n      sha256 "%s"' "$base" "$file" "$hash"
}

darwin_arm64=$(block darwin_arm64)
darwin_amd64=$(block darwin_amd64)
linux_arm64=$(block linux_arm64)
linux_amd64=$(block linux_amd64)

cat <<RUBY
class Amplifi < Formula
  desc "Inspect and control Ubiquiti AmpliFi routers over the local control protocol"
  homepage "https://github.com/borud/amplifi"
  license "Apache-2.0"

  on_macos do
    on_arm do
${darwin_arm64}
    end
    on_intel do
${darwin_amd64}
    end
  end

  on_linux do
    on_arm do
${linux_arm64}
    end
    on_intel do
${linux_amd64}
    end
  end

  def install
    bin.install "amplifi"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/amplifi --version")
  end
end
RUBY
