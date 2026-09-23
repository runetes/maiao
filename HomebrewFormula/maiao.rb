class Maiao < Formula
  desc "Seamless GitHub PR management from the command-line"
  homepage "https://github.com/runetes/maiao"
  url "https://github.com/runetes/maiao.git",
    tag:      "maiao-v1.7.0",
    revision: "9d5236e20f0e8f0c2f679a04c0ccb9369defb0b6"
  license "MIT"
  conflicts_with "git-review"
  head "https://github.com/runetes/maiao.git",
    branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X github.com/runetes/maiao/pkg/version.Version=#{version}+homebrew-runetes-maiao
    ]

    # The macOS Keychain backend of 99designs/keyring is behind a
    # `darwin && cgo` build tag. Without cgo the binary has no Keychain at all
    # and every credential lookup fails, so this is not left to the default.
    ENV["CGO_ENABLED"] = "1"

    system "go", "build", *std_go_args(ldflags: ldflags, output: bin/"git-review"), "./cmd/maiao"
    generate_completions_from_executable(bin/"git-review", "completion")
  end

  test do
    assert_match "#{version}+homebrew-runetes-maiao", shell_output("#{bin}/git-review version")
  end
end
