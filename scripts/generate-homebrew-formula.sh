#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <version> <checksums-file> <output-file>" >&2
  exit 2
fi

version="$1"
checksums_file="$2"
output_file="$3"
release_version="${version#v}"
base_url="https://github.com/AndrzejKrzywda00/assistant/releases/download/${version}"

checksum_for() {
  local asset="$1"
  local checksum
  checksum="$(awk -v name="${asset}" '$2 == ("./" name) || $2 == name { print $1 }' "${checksums_file}")"
  if [[ ! "${checksum}" =~ ^[0-9a-f]{64}$ ]]; then
    echo "missing or invalid checksum for ${asset}" >&2
    exit 1
  fi
  printf '%s' "${checksum}"
}

darwin_arm64="assistant_${version}_Darwin_arm64.tar.gz"
darwin_amd64="assistant_${version}_Darwin_x86_64.tar.gz"
linux_arm64="assistant_${version}_Linux_arm64.tar.gz"
linux_amd64="assistant_${version}_Linux_x86_64.tar.gz"

sha_darwin_arm64="$(checksum_for "${darwin_arm64}")"
sha_darwin_amd64="$(checksum_for "${darwin_amd64}")"
sha_linux_arm64="$(checksum_for "${linux_arm64}")"
sha_linux_amd64="$(checksum_for "${linux_amd64}")"

mkdir -p "$(dirname "${output_file}")"
cat > "${output_file}" <<FORMULA
class Assistant < Formula
  desc "Keyboard-first local productivity assistant"
  homepage "https://github.com/AndrzejKrzywda00/assistant"
  version "${release_version}"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "${base_url}/${darwin_arm64}"
      sha256 "${sha_darwin_arm64}"
    else
      url "${base_url}/${darwin_amd64}"
      sha256 "${sha_darwin_amd64}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${base_url}/${linux_arm64}"
      sha256 "${sha_linux_arm64}"
    else
      url "${base_url}/${linux_amd64}"
      sha256 "${sha_linux_amd64}"
    end
  end

  def install
    bin.install "assistant"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/assistant version")
  end
end
FORMULA
