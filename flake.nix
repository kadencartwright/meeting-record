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
        in
        {
          default = pkgs.buildGoModule {
            pname = "meeting-record";
            version = "0.1.0";
            src = self;
            vendorHash = null;

            nativeBuildInputs = [ pkgs.makeWrapper ];
            postInstall = ''
              wrapProgram $out/bin/meeting-record \
                --prefix PATH : ${pkgs.lib.makeBinPath [
                  pkgs.pipewire
                  pkgs.wireplumber
                  pkgs.xdg-utils
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
              pkgs.pipewire
              pkgs.wireplumber
            ];
          };
        }
      );
    };
}

