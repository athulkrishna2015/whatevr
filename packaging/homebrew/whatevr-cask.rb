cask "whatevr" do
  arch arm: "arm64", intel: "amd64"

  version "0.8.0"
  sha256 arm:   "0000000000000000000000000000000000000000000000000000000000000000",
         intel: "0000000000000000000000000000000000000000000000000000000000000000"

  url "https://github.com/codelif/whatevr/releases/download/v#{version}/whatevr-#{version}-darwin-#{arch}.tar.gz"
  name "Whatevr"
  desc "Native WhatsApp client (whatevrd daemon + whattui terminal frontend)"
  homepage "https://github.com/codelif/whatevr"

  depends_on formula: "jpeg-turbo"
  depends_on macos: :ventura

  app "whatevr-#{version}-darwin-#{arch}/Whatevr.app"
  binary "#{appdir}/Whatevr.app/Contents/MacOS/whatevrd"
  binary "whatevr-#{version}-darwin-#{arch}/whattui"

  generate_completions_from_executable "#{appdir}/Whatevr.app/Contents/MacOS/whatevrd", "completion"

  # ad-hoc signed, not notarized: gatekeeper would refuse it. preflight, so
  # the completions above run a binary it lets through
  preflight_steps do
    run "/usr/bin/xattr", args: ["-d", "-r", "com.apple.quarantine", "{{staged_path}}"], must_succeed: false
  end
  postflight_steps do
    run "/usr/bin/xattr", args: ["-d", "-r", "com.apple.quarantine", "{{appdir}}/Whatevr.app"], must_succeed: false
  end

  uninstall launchctl: "in.codelif.whatevr.daemon",
            quit:      "in.codelif.whatevr"

  zap trash: [
    "~/Library/Application Support/in.codelif.whatevr",
    "~/Library/Caches/in.codelif.whatevr",
    "~/Library/LaunchAgents/in.codelif.whatevr.daemon.plist",
    "~/Library/Logs/in.codelif.whatevr",
  ]

  caveats <<~EOS
    Start the daemon at login:
      whatevrd service enable

    ffmpeg adds video posters and voice note waveforms:
      brew install ffmpeg
  EOS
end
