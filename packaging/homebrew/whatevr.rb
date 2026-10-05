class Whatevr < Formula
  desc "Native WhatsApp client (whatevrd daemon + whattui terminal frontend)"
  homepage "https://github.com/codelif/whatevr"
  url "https://github.com/codelif/whatevr/releases/download/v0.8.0/whatevr-0.8.0.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  license "BSD-3-Clause"
  head "https://github.com/codelif/whatevr.git", branch: "main"

  depends_on "go" => :build
  depends_on "just" => :build
  depends_on "pkgconf" => :build
  depends_on "jpeg-turbo"
  depends_on :macos
  depends_on "ffmpeg" => :recommended

  uses_from_macos "python" => :build

  def install
    system "just", "install", prefix
  end

  def caveats
    <<~EOS
      Start the daemon at login:
        whatevrd service enable

      To find Whatevr in Spotlight and Launchpad:
        ln -sf #{opt_prefix}/Whatevr.app ~/Applications/
    EOS
  end

  test do
    assert_match "in.codelif.whatevr", shell_output("#{bin}/whatevrd paths")
    assert_path_exists prefix/"Whatevr.app/Contents/MacOS/Whatevr"
    system "codesign", "--verify", "--deep", "--strict", prefix/"Whatevr.app"
  end
end
