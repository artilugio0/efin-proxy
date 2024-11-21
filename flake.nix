{
  description = "Go shell";

  inputs = {
    nixpkgs.url = "nixpkgs/nixos-unstable";
  };

  outputs = { nixpkgs, ... } :
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        packages = with pkgs; [
          go
          gopls
          gotools
          protobuf
          protoc-gen-go
          protoc-gen-go-grpc
          sqlite
          grpcurl
        ];
      };
    };
}
