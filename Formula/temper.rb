# frozen_string_literal: true

class Temper < Formula
  desc "Install and manage reproducible local AI configurations"
  homepage "https://github.com/temper-sh/temper"
  url "https://github.com/temper-sh/temper/archive/refs/tags/v0.5.0.tar.gz"
  sha256 "1c11acaeb09841249504b480a972b4279fc7ded06121012d46d508bcd473a62a"
  license "0BSD"

  bottle do
    root_url "https://github.com/temper-sh/temper/releases/download/v0.5.0"
    sha256 cellar: :any_skip_relocation, arm64_sequoia: "8e1e3727b086dbc17a19e25266ab44ce93d262835dd59f37f79cc9224f0c898e"
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
