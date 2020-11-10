#!/usr/bin/env bash

################################################################################
# valet.sh
#
# Copyright: (C) 2018 TechDivision GmbH - All Rights Reserved
# Author: Johann Zelger <j.zelger@techdivision.com>
# Author: Florian Schmid <f.schmid@techdivision.com>
################################################################################

# Set a global trap for e.g. ctrl+c to run shutdown routine
trap shutdown SIGINT

# track start time
APPLICATION_START_TIME=$(date +%s);

# define application to be auto startet in case of testing purpose for example
: "${APPLICATION_AUTOSTART:=1}"
# define default spinner enabled
: "${SPINNER_ENABLED:=1}"
# define application return codes
APPLICATION_RETURN_CODE_ERROR=255
APPLICATION_RETURN_CODE_SUCCESS=0
# set default return code 0
APPLICATION_RETURN_CODE=$APPLICATION_RETURN_CODE_SUCCESS
# define default setting for debug info output
APPLICATION_DEBUG_INFO_ENABLED=0;
# define default help behaviour output
APPLICATION_HELP_INFO_ENABLED=0;
# define default force behaviour
APPLICATION_FORCE_INFO_ENABLED=0;
# define variables
APPLICATION_NAME="valet.sh"
# define default git relevant variables
APPLICATION_GIT_NAMESPACE=${APPLICATION_GIT_NAMESPACE:="valet-sh"}
APPLICATION_GIT_REPOSITORY=${APPLICATION_GIT_REPOSITORY:="valet-sh"}
APPLICATION_GIT_URL=${APPLICATION_GIT_URL:="https://github.com/${APPLICATION_GIT_NAMESPACE}/${APPLICATION_GIT_REPOSITORY}"}
# semver validator regex
SEMVER_REGEX='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\-!?[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
# define default playbook dir
ANSIBLE_PLAYBOOKS_DIR="playbooks"
# define application prefix path
APPLICATION_PREFIX_PATH="/usr/local"
# define default install directory
APPLICATION_REPO_DIR="${APPLICATION_PREFIX_PATH}/${APPLICATION_GIT_NAMESPACE}/${APPLICATION_GIT_REPOSITORY}"
# use current bash source script dir as base_dir
BASE_DIR=${BASE_DIR:=${APPLICATION_REPO_DIR}}

# check if git dir is available in base dir
if [ -d "${BASE_DIR}/.git" ]; then
    # get the current version from git repository in base dir
    APPLICATION_VERSION=$(git --git-dir="${BASE_DIR}/.git" --work-tree="${BASE_DIR}" describe --tags)
fi

##############################################################################
# Toggles spinner animation
##############################################################################
spinner_toggle() {
    # check if spinner animation is globally enabled
    if [ "${SPINNER_ENABLED}" != "1" ]
    then
        return 0
    fi
    # start spinner background function
    function _spinner_start() {
        local list=( "$(echo -e '\xe2\xa0\x8b')"
                     "$(echo -e '\xe2\xa0\x99')"
                     "$(echo -e '\xe2\xa0\xb9')"
                     "$(echo -e '\xe2\xa0\xb8')"
                     "$(echo -e '\xe2\xa0\xbc')"
                     "$(echo -e '\xe2\xa0\xb4')"
                     "$(echo -e '\xe2\xa0\xa6')"
                     "$(echo -e '\xe2\xa0\xa7')"
                     "$(echo -e '\xe2\xa0\x87')"
                     "$(echo -e '\xe2\xa0\x8f')" )
        local i=0
        tput sc
        while true; do
            printf "\\e[32m%s\\e[39m $1 " "${list[i]}"
            i=$((i+1))
            i=$((i%10))
            sleep 0.1
            tput rc
        done
    }
    # stop spinner background function
    function _spinner_stop() {
        kill "$SPINNER_PID" > /dev/null 2>&1
        wait "$!" 2>/dev/null
        SPINNER_PID=0
        tput rc
    }
    # check if spinner pid doen not exist
    if [[ "$SPINNER_PID" -lt 1 ]]; then
        tput sc
        _spinner_start "$1" &
        SPINNER_PID="$!"
    else
        _spinner_stop
    fi
}

##############################################################################
# Logs messages in given type
##############################################################################
function out() {
    case "${1--h}" in
        error) printf "\\033[1;31m✘ %s\\033[0m\\n" "$2";;
        warning) printf "\\033[1;33m⚠ %s\\033[0m\\n" "$2";;
        success) printf "\\033[1;32m✔ %s\\033[0m\\n" "$2";;
        task) printf "▸ %s\\n" "$2";;
        *) printf "%s\\n" "$*";;
    esac
}

#######################################
# Validates version against semver
# Globals:
#   None
# Arguments:
#   Version
# Returns:
#   None
#######################################
function version_validate() {
    local version=$1
    if [[ "$version" =~ ${SEMVER_REGEX} ]]; then
        if [ "$#" -eq "2" ]; then
            local major=${BASH_REMATCH[1]}
            local minor=${BASH_REMATCH[2]}
            local patch=${BASH_REMATCH[3]}
            local prere=${BASH_REMATCH[4]}
            local build=${BASH_REMATCH[5]}
            eval "$2=(\"$major\" \"$minor\" \"$patch\" \"$prere\" \"$build\")"
        else
            echo "$version"
        fi
    else
        out error "Version $version does not match the semver scheme 'X.Y.Z(-PRERELEASE)(+BUILD)'. See help for more information." error
    fi
}

##############################################################################
# Compares versions
##############################################################################
function version_compare() {
    version_validate "$1" V
    version_validate "$2" V_

    for i in 0 1 2; do
        local diff=$((${V[$i]} - ${V_[$i]}))
        if [[ $diff -lt 0 ]]; then
            echo -1; return 0
        elif [[ $diff -gt 0 ]]; then
            echo 1; return 0
        fi
    done

    if [[ -z "${V[3]}" ]] && [[ -n "${V_[3]}" ]]; then
        echo -1; return 0;
    elif [[ -n "${V[3]}" ]] && [[ -z "${V_[3]}" ]]; then
        echo 1; return 0;
    elif [[ -n "${V[3]}" ]] && [[ -n "${V_[3]}" ]]; then
        if [[ "${V[3]}" > "${V_[3]}" ]]; then
            echo 1; return 0;
        elif [[ "${V[3]}" < "${V_[3]}" ]]; then
          echo -1; return 0;
        fi
    fi

    echo 0
}

##############################################################################
# Prepares application by installing dependencies and itself
##############################################################################
function prepare() {
    # check if root user is acting
    if [[ ${EUID:-$(id -u)} -eq 0 ]]; then error "Please do not run valet.sh as root"; fi
    # set cwd to base dir
    cd "${BASE_DIR}" || error "Unable to set cwd to ${BASE_DIR}"
}

##############################################################################
# Upgrade meachanism of valet.sh itself
##############################################################################
function self_upgrade() {
    # create version map to extract major, minor and build parts later on
    version_validate "${APPLICATION_VERSION}" APPLICATION_VERSION_MAP
    # define default git tag filter based on major version
    GIT_TAG_FILTER="^${APPLICATION_VERSION_MAP[0]}.*";
    # check if force self_upgrade was triggered
    if [ $APPLICATION_FORCE_INFO_ENABLED = 1 ]; then
        out warning "CAUTION! This will trigger a major version update if it's available."
        read -r -p "Are You Sure? [Y/n] " input
        echo "";
        case $input in
            [yY][eE][sS]|[yY])
            GIT_TAG_FILTER=".*"
        ;;
            [nN][oO]|[nN])
        exit 1
        ;;
            *)
        out error "Invalid input '$input'"
        shutdown
        ;;
        esac
    fi

    # trigger sudo password check
    sudo true
    # start spinner
    spinner_toggle "Upgrading"
    # fetch all tags from application git repo
    git --git-dir="${APPLICATION_REPO_DIR}/.git" --work-tree="${APPLICATION_REPO_DIR}" fetch --tags --quiet
    # get available release tags sorted by refname
    GIT_TAGS=$(git --git-dir="${APPLICATION_REPO_DIR}/.git" --work-tree="${APPLICATION_REPO_DIR}" tag --sort "-v:refname" | grep "${GIT_TAG_FILTER}")
    # get latest semver conform git version tag
    for GIT_TAG in ${GIT_TAGS}; do
        if [[ "${GIT_TAG}" =~ ${SEMVER_REGEX} ]]; then
            git --git-dir="${APPLICATION_REPO_DIR}/.git" --work-tree="${APPLICATION_REPO_DIR}" checkout --force --quiet "${GIT_TAG}"
            break
        fi
    done

    # update dependencies based on os type
    if [[ "$OSTYPE" == "linux-gnu" ]]; then
        sudo pip3 install -q -r ${APPLICATION_REPO_DIR}/requirements.txt> /dev/null 2>&1
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        sudo pip install -q -r ${APPLICATION_REPO_DIR}/requirements.txt > /dev/null 2>&1
    fi

    # stop spinner
    spinner_toggle
    # output specific upgrade status message
    if [ "$(version_compare "${APPLICATION_VERSION}" "${GIT_TAG}")" = "0" ]; then
        out success "Already on the latest version $GIT_TAG"
    else
        out success "Successfully upgraded from ${APPLICATION_VERSION} to latest version ${GIT_TAG}"
    fi
}

##############################################################################
# Prints the console tool header
##############################################################################
function print_header() {
    echo -e "\\033[1m$APPLICATION_NAME\\033[0m \\033[34m$APPLICATION_VERSION\\033[0m"
    printf "\\n"
}

##############################################################################
# Prints the console tool header
##############################################################################
function print_footer() {
    LC_NUMERIC="en_US.UTF-8"

    APPLICATION_END_TIME=$(date +%s)
    APPLICATION_EXECUTION_TIME=$(echo "$APPLICATION_END_TIME - $APPLICATION_START_TIME" | bc);

    if [ $APPLICATION_DEBUG_INFO_ENABLED = 1 ]; then
        printf "\\n"
        printf "\\e[34m"
        printf "\\e[1mDebug information:\\033[0m"
        printf "\\e[34m"
        printf "\\n"
        printf "  Version: \\e[1m%s\\033[0m\\n" "$APPLICATION_VERSION"
        printf "\\e[34m"
        printf "  Execution time: \\e[1m%f sec.\\033[0m\\n" "$APPLICATION_EXECUTION_TIME"
        printf "\\e[34m"
        printf "  Exitcode: \\e[1m%s\\033[0m\\n" "$APPLICATION_RETURN_CODE"
        printf "\\e[34m\\033[0m"
        printf "\\n"
    fi
}

##############################################################################
# Print usage help and command list
##############################################################################
function print_usage() {
    local cmd_output_space='                '
    local cmd_name="-x"

    # show general help if no specific command was given
    if [[ -z "$1" ]]; then

        printf "\\e[33mUsage:\\e[39m\\n"
        printf "  command [options] [command] [arguments]\\n"
        printf "\\n"
        printf "\\e[33mOptions:\\e[39m\\n"
        printf "\\e[32m  -h %s \\e[39mDisplay this help message\\n" "${cmd_output_space:${#cmd_name}}"
        printf "\\e[32m  -v %s \\e[39mDisplay this application version\\n" "${cmd_output_space:${#cmd_name}}"
        printf "\\e[32m  -d %s \\e[39mDisplay debug information\\n" "${cmd_output_space:${#cmd_name}}"
        printf "\\n"
        printf "\\e[33mCommands:\\e[39m\\n"

        local cmd_name="self-upgrade"
        local cmd_description="Upgrade valet.sh itself to latest version."
        printf "  \\e[32m%s %s \\e[39m${cmd_description}\\n" "${cmd_name}" "${cmd_output_space:${#cmd_name}}"

        if [ -d "$BASE_DIR/playbooks" ]; then
            for file in ./playbooks/**.yml; do
                local cmd_name
                cmd_name="$(basename "${file}" .yml)"
                local cmd_description
                cmd_description=$(grep '^\#[[:space:]]@description:' -m 1 "${file}" | awk -F'"' '{ print $2}');
                if [ -n "${cmd_description}" ]; then
                    printf "  \\e[32m%s %s \\e[39m${cmd_description}\\n" "${cmd_name}" "${cmd_output_space:${#cmd_name}}"
                fi
            done
        fi
        
        printf "\\n"
        
    else
        # parse command specific playbook if command was given
        cmd_file="$BASE_DIR/playbooks/$1.yml"
        cmd_type=""
        cmd_help=""

        # check if requested playbook yml exist and execute it
        if [ ! -f "$cmd_file" ]; then
            out error "Command '$1' not available"
            shutdown
        fi

        # parse playbook file for comment header informations
        while read -r line; do
            if [[ ${line} == "---" ]] ; then
                break
            fi
            if [[ ${line} = "# @command:"*  ]] ; then
                cmd_type="command"
                cmd_name=$(echo "${line}" | grep '^\#[[:space:]]@command:' -m 1 | awk -F'"' '{ print $2}');
                continue
            fi
            if [[ ${line} = "# @description:"*  ]] ; then
                cmd_type="description"
                cmd_description=$(echo "${line}" | grep '^\#[[:space:]]@description:' -m 1 | awk -F'"' '{ print $2}');
                continue
            fi
            if [[ ${line} = "# @usage:"*  ]] ; then
                cmd_usage=$(echo "${line}" | grep '^\#[[:space:]]@usage:' -m 1 | awk -F'"' '{ print $2}');
                cmd_type="usage"
                continue
            fi
            if [[ ${line} = "# @help:"*  ]] ; then
                cmd_type="help"
                continue
            fi
            if [[ ${cmd_type} == "help" ]] ; then
                cmd_help+="  "
                cmd_help+=$(echo "${line}" | awk -F'# ' '{ print $2}');
                cmd_help+=$'\n'
            fi
        done < "${cmd_file}"

        printf "\\e[33mCommand:\\e[39m\\e[32m %s\\e[39m\\n" "${cmd_name}"
        printf "  %s\\n" "${cmd_description}"
        printf "\\n"
        printf "\\e[33mUsage:\\e[39m\\n"
        printf "  %s\\n" "${cmd_usage}"
        printf "\\n"
        printf "\\e[33mHelp:\\e[39m\\n"
        printf "%s\\n" "${cmd_help}"
    fi
}

##############################################################################
# Executes command via ansible playbook
##############################################################################
function execute_ansible_playbook() {
    local command=$1
    local ansible_playbook_file="$ANSIBLE_PLAYBOOKS_DIR/$command.yml"
    local ansible_options=""
    local parsed_args=$2
    local parsed_opts=$3

    # define complete extra vars object
    read -r -d '' ansible_extra_vars << EOM
{
    "cli": {
        "name": "${APPLICATION_NAME}",
        "version": "${APPLICATION_VERSION}",
        "args": [${parsed_args}],
        "opts": [${parsed_opts}]
    }
}
EOM

    ansible_extra_vars=("--extra-vars" "${ansible_extra_vars}")

    # check if requested playbook yml exist and execute it
    if [ -f "$ansible_playbook_file" ]; then

        # check if debug was enabled and set correct ansible optionsy
        if [ "$APPLICATION_DEBUG_INFO_ENABLED" = 1 ]; then
            ansible_options="-v"
        fi

        # execute ansible-playbook
        ansible-playbook ${ansible_options} "${ansible_playbook_file}" "${ansible_extra_vars[@]}" || APPLICATION_RETURN_CODE=$?
        
    else
        out error "Command '$command' not available"
    fi
}

##############################################################################
# Error handling and abort function with log message
##############################################################################
function error() {
    # check if error message is given
    if [ -z "$*" ]; then
        echo "no error message given"
        shutdown $APPLICATION_RETURN_CODE_ERROR
    fi
    # output error message to user
    out error "$*"    
    # trigger immediate shutdown
    shutdown $APPLICATION_RETURN_CODE_ERROR
}

##############################################################################
# Shutdown cli client script
##############################################################################
function shutdown() {
    # kill spinner by pid
    if [[ -n "${SPINNER_PID}" && "${SPINNER_PID}" -gt 0 ]]; then
        kill -9 "${SPINNER_PID}" &> /dev/null
        wait "$!" 2>/dev/null
    fi

    # exit
    if [ "$1" ]; then
        APPLICATION_RETURN_CODE=$1
    fi
    exit "${APPLICATION_RETURN_CODE}"
}

##############################################################################
# Process all bash args given from shell
##############################################################################
function process_args() {

    local parsed_command=""
    local parsed_args=""
    local parsed_opts=""

    # check if no arguments were given
    if [ $# -eq 0 ];
    then
        # just display usage in case of zero arguments
        print_usage
    else
        # parse options first and handle it
        for i in "$@"; do

            # parse double dash options (ansible)
            if [[ ${i:0:2} == "--" ]]; then
                if [ ${#parsed_opts} -gt 0 ]; then
                    parsed_opts+=,;
                fi
                parsed_opts+="\"${i}\"";
                shift
                continue
            fi

            # parse single dash options (cli)
            if [[ ${i:0:1} == "-" ]]; then
                if [ ${#parsed_opts} -gt 0 ]; then
                    parsed_opts+=,;
                fi
                parsed_opts+="\"${i}\"";
                case ${i} in
                    -d)
                        # enable debug info
                        export APPLICATION_DEBUG_INFO_ENABLED=1
                        shift
                        ;;
                    -v)
                        # immediate shutdown to display version only
                        shutdown
                        shift
                        ;;
                    -h)
                        # print usage for help then shutdown
                        export APPLICATION_HELP_INFO_ENABLED=1
                        shift
                        ;;
                    -f)
                        # enable force info
                        export APPLICATION_FORCE_INFO_ENABLED=1
                        shift
                        ;;
                    -*)
                        shift
                        # error in this case
                        error "Invalid option: ${i}"
                esac

            # parse command and given args
            else
                if [ ${#parsed_command} -gt 0 ]; then
                    if [ ${#parsed_args} -gt 0 ]; then
                        parsed_args+=,;
                    fi
                    parsed_args+="\"${i}\""
                else
                    parsed_command=$1;
                fi
            fi
        done;

        # if help info was enabled by "-h" output help
        if [ "$APPLICATION_HELP_INFO_ENABLED" = 1 ];
        then
            print_usage "${parsed_command}"
            shutdown
        fi

        # handle remaining args if given
        if [ -n "$*" ]; then
            case "${1--h}" in
                self-upgrade) self_upgrade;;
                # try to execute playbook based on command
                # ansible will throw an error if specific playbook does not exist
                *) execute_ansible_playbook "$parsed_command" "$parsed_args" "$parsed_opts";;
            esac
        else
            print_usage
        fi
    fi
}

##############################################################################
# Main
##############################################################################
function main() {
    prepare
    print_header
    process_args "$@"
    print_footer
    shutdown
}

# start cli with given command line args if autostart is enabled
if [ "${APPLICATION_AUTOSTART}" = "1" ]; then
    main "$@";
fi
