# pwt shell integration (POSIX sh; verified with bash, zsh and dash)
#
# pwt itself is a normal binary and works without this file. Sourcing it
# only adds the ability to change the shell's current directory, which a
# child process cannot do on its own.
#
#   source /path/to/pwt.sh
#
# Without it, move like this instead:
#
#   cd "$(pwt path 2)"
#
# The function forwards everything to the binary. For subcommands that may
# move you, it passes a temporary file through PWT_CD_FILE; the binary
# writes the destination there and the function performs the cd.

pwt() {
    case "${1-}" in
        switch | remove) ;;
        *)
            command pwt "$@"
            return $?
            ;;
    esac

    # local は POSIX にないが、dash / ash を含め実用上すべての sh が実装している。
    # 変数名は zsh の特殊変数 (status / path) を避けて付けている。
    local cd_file rc dest
    cd_file="$(mktemp "${TMPDIR:-/tmp}/pwt-cd.XXXXXX")" || return 1

    PWT_CD_FILE="$cd_file" command pwt "$@"
    rc=$?

    if [ -s "$cd_file" ]; then
        dest="$(cat "$cd_file")"
        rm -f "$cd_file"
        if [ -d "$dest" ]; then
            cd "$dest" || return 1
        fi
    else
        rm -f "$cd_file"
    fi

    return $rc
}
