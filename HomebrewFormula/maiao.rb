class Maiao < Formula
  desc "Seamless GitHub PR management from the command-line"
  homepage "https://github.com/runetes/maiao"
  url "https://github.com/runetes/maiao.git",
    tag:      "maiao-v1.6.0",
    revision: "a4832d1272dd9269bbadec5ad677bec4497c0bb4"
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
