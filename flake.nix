{
  description = "Passive PipeWire meeting recorder for Linux";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          notionPlatform = {
            x86_64-linux = "linux-x64";
            aarch64-linux = "linux-arm64";
          }.${system};
          notionCli = pkgs.stdenvNoCC.mkDerivation {
            pname = "notion-cli";
            version = "0.23.1";
            src = pkgs.fetchurl {
              url = "https://registry.npmjs.org/ntn/-/ntn-0.23.1.tgz";
              hash = "sha256-VG5RxGbvczsins1ilD3aIPeVYfDabZIvXIKWja5BTTc=";
            };
            sourceRoot = "package";
            dontStrip = true;
            installPhase = ''
              runHook preInstall
              install -Dm755 dist/ntn-${notionPlatform}/ntn $out/bin/ntn
              runHook postInstall
            '';
            meta = {
              description = "Official Notion command-line interface";
              homepage = "https://developers.notion.com/cli/get-started/overview";
              license = pkgs.lib.licenses.mit;
              mainProgram = "ntn";
              platforms = pkgs.lib.platforms.linux;
            };
          };
          meetingRecord = pkgs.buildGoModule {
            pname = "meeting-record";
            version = "0.3.0";
            src = self;
            vendorHash = null;

            nativeBuildInputs = [ pkgs.makeWrapper ];
            postInstall = ''
              wrapProgram $out/bin/meeting-record \
                --prefix PATH : ${pkgs.lib.makeBinPath [
                  pkgs.ffmpeg-headless
                  pkgs.pipewire
                  pkgs.systemd
                  pkgs.wireplumber
                  pkgs.xdg-utils
                  notionCli
                ]}
            '';

            meta = {
              description = "Passive recorder for a default PipeWire microphone and output sink monitor";
              homepage = "https://github.com/kadencartwright/meeting-record";
              license = pkgs.lib.licenses.mit;
              mainProgram = "meeting-record";
              platforms = pkgs.lib.platforms.linux;
            };
          };
        in
        {
          default = meetingRecord;
          meeting-record = meetingRecord;
          notion-cli = notionCli;
        }
      );

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/meeting-record";
        };
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.go
              pkgs.gopls
              pkgs.ffmpeg-headless
              pkgs.pipewire
              pkgs.wireplumber
            ];
          };
        }
      );
    };
}
