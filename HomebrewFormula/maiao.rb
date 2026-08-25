class Maiao < Formula
  desc "Seamless GitHub PR management from the command-line"
  homepage "https://github.com/runetes/maiao"
  url "https://github.com/runetes/maiao.git",
    tag:      "maiao-v1.4.0",
    revision: "2e6b72857b09a8b38a1ebb7ea6b0e04d8996fb84"
  license "MIT"
  conflicts_with "git-review"
  head "https://github.com/runetes/maiao.git",
    branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X github.com/adevinta/maiao/pkg/version.Version=#{version}+homebrew-runetes-maiao
    ]

    system "go", "build", *std_go_args(ldflags: ldflags, output: bin/"git-review"), "./cmd/maiao"
    generate_completions_from_executable(bin/"git-review", "completion")
  end

  test do
    assert_match "#{version}+homebrew-runetes-maiao", shell_output("#{bin}/git-review version")
  end
end
