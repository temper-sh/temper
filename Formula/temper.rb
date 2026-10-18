# frozen_string_literal: true

class Temper < Formula
  desc "Install and manage reproducible local AI configurations"
  homepage "https://github.com/temper-sh/temper"
  url "https://github.com/temper-sh/temper/archive/refs/tags/v0.3.1.tar.gz"
  sha256 "880ae6630302d513f4e16b506aeb5b244f92b7705681ac05859e766c3efef552"
  license "0BSD"

  bottle do
    root_url "https://github.com/temper-sh/temper/releases/download/v0.3.1"
    sha256 cellar: :any_skip_relocation, arm64_sequoia: "802dd4f5c6e75f5eaebe38a3ff64948c9a0cbcd6cf4efda01304cfc7947f1455"
  end

  depends_on "go" => :build
  depends_on arch: :arm64
  depends_on :macos

  def install
    system "go", "run", "./cmd/temper-release", "build",
           "--version", version.to_s, "--output", "build/temper"
    system "go", "run", "./cmd/temper-release", "package",
           "--version", version.to_s, "--binary", "build/temper", "--output", "dist"

    bin.install "build/temper"
    prefix.install "LICENSE"
    system "/usr/bin/unzip", "-j", "dist/temper_#{version}_darwin_arm64.zip",
           "*/THIRD_PARTY_NOTICES.txt", "-d", "notices"
    pkgshare.install "notices/THIRD_PARTY_NOTICES.txt"
  end

  test do
    assert_equal "temper #{version}", shell_output("#{bin}/temper version").strip
    root = testpath/"temper-state"
    result = JSON.parse(shell_output("#{bin}/temper configure --root #{root} --show --json"))
    assert_equal "temper-configuration/v1", result.fetch("configuration").fetch("schema")
    assert_empty result.fetch("configuration").fetch("presets")
    assert_empty result.fetch("configuration").fetch("layouts")
    assert_equal "", result.fetch("revision")
    assert_path_exists pkgshare/"THIRD_PARTY_NOTICES.txt"
    refute_path_exists root
  end
end
