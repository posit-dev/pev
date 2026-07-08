BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m'

# Save the list of systemd unit files to a variable to avoid running systemctl multiple times
UNIT_FILES=$(systemctl list-unit-files)

echo -e ${BLUE}"Hostname: "${NC}`hostname` && \
echo -e -n ${BLUE}"OS Version: "${NC} && \
echo -e "$(grep -E '^(PRETTY_NAME)=' /etc/os-release | cut -d= -f2 | tr -d '"')" && \
echo -e ${BLUE}"IP Addresses: "${NC} && ip -o -4 addr show | awk -v BLUE="$BLUE" -v NC="$NC" '!/127.0.0.1/ {print "   " BLUE $2 NC ": " $4}'  && \
echo -e ${BLUE}"Memory Utilization:${NC} $(free -h | awk '/^Mem:/ {print $3 "/" $2}')" && \
echo -e ${BLUE}"CPU:${NC} $(lscpu | grep 'Model name' | awk -F: '{print $2}' | sed 's/^ *//')" && \
echo -e ${BLUE}"   CPU Cores:"${NC} $(nproc) && \
echo -e ${BLUE}"Mounted Directories and Storage:"${NC} && \
df -h | head -n 1 | GREP_COLORS='mt=01;32' grep -E --color 'K|M|G|T|Avail|Size|Filesystem|Use%|Used|Mounted on' && \
df -h | tail -n +2 | sort -k6 | GREP_COLORS='mt=01;32' grep -E --color 'K|M|G|T|Avail|Size|Filesystem|Use%|Used|Mounted on' && \

# Posit Software
echo -e ${BLUE}"Checking Posit Software:${NC}" && \
for svc in rstudio-server rstudio-launcher rstudio-connect rstudio-pm; do
    if echo "$UNIT_FILES" | grep -q "^$svc.service"; then
        status=$(systemctl is-active $svc 2>/dev/null)

        # Get version using software-specific CLI commands
        PKG_VERSION=""
        case $svc in
            rstudio-server)
                PKG_VERSION=$(rstudio-server version 2>/dev/null | head -1 | grep -oE '[0-9]{4}\.[0-9]{2}\.[0-9]+' | head -1)
                ;;
            rstudio-connect)
                PKG_VERSION=$(/opt/rstudio-connect/bin/connect --version 2>/dev/null | head -1 | grep -oE '[0-9]{4}\.[0-9]{2}\.[0-9]+' | head -1)
                ;;
            rstudio-pm)
                if [[ -x /opt/rstudio-pm/bin/rspm ]]; then
                    PKG_VERSION=$(/opt/rstudio-pm/bin/rspm --version </dev/null 2>/dev/null | head -1 | grep -oE '[0-9]{4}\.[0-9]{2}\.[0-9]+' | head -1)
                elif command -v rspm &> /dev/null; then
                    PKG_VERSION=$(command rspm --version </dev/null 2>/dev/null | head -1 | grep -oE '[0-9]{4}\.[0-9]{2}\.[0-9]+' | head -1)
                fi
                ;;
        esac

        if [[ "$status" == "active" ]]; then
            if [[ -n "$PKG_VERSION" ]]; then
                echo -e "   ${BLUE}$svc:${GREEN} Installed ($PKG_VERSION)${NC}"
            else
                echo -e "   ${BLUE}$svc:${GREEN} Installed${NC}"
            fi
        else
            if [[ -n "$PKG_VERSION" ]]; then
                echo -e "   ${BLUE}$svc:${YELLOW} Installed but Inactive ($PKG_VERSION)${NC}"
            else
                echo -e "   ${BLUE}$svc:${YELLOW} Installed but Inactive${NC}"
            fi
        fi
    else
        echo -e "   ${BLUE}$svc:${RED} Not Installed${NC}"
    fi
done && \

# Check Posit Pro-Drivers
if [[ -d "/opt/rstudio-drivers" ]]; then
    # Try dpkg first (Debian/Ubuntu)
    if command -v dpkg &> /dev/null; then
        DRIVERS_VERSION=$(dpkg -l 2>/dev/null | grep "^ii  rstudio-drivers " | awk '{print $3}')
    # Try rpm (RHEL/CentOS/SUSE)
    elif command -v rpm &> /dev/null; then
        DRIVERS_VERSION=$(rpm -q rstudio-drivers 2>/dev/null | sed 's/rstudio-drivers-//' | cut -d'-' -f1)
    fi

    if [[ -n "$DRIVERS_VERSION" ]]; then
        echo -e "   ${BLUE}rstudio-drivers:${GREEN} Installed ($DRIVERS_VERSION)${NC}"
    else
        echo -e "   ${BLUE}rstudio-drivers:${YELLOW} Directory exists but package not detected${NC}"
    fi
else
    echo -e "   ${BLUE}rstudio-drivers:${RED} Not Installed${NC}"
fi && \

# List installed R versions in /opt/R (only versions starting with 3 or 4)
echo -e ${BLUE}"Checking Installed R Versions (/opt/R):"${NC} && \
if [[ -d "/opt/R" ]]; then
    R_VERSIONS=$(ls -r /opt/R | grep -E '^[34]' | tr '\n' ',' | sed 's/,$//')
    if [[ -z "$R_VERSIONS" ]]; then
        echo -e "   ${RED}No Posit R versions installed${NC}"
    else
        echo -e "   ${GREEN}$R_VERSIONS${NC}"
    fi
else
    echo -e "   ${RED}No Posit R versions installed${NC}"
fi && \

# List installed Python versions in /opt/python (only versions starting with 3 or 4)
echo -e ${BLUE}"Checking Installed Python Versions (/opt/python):"${NC} && \
if [[ -d "/opt/python" ]]; then
    PYTHON_VERSIONS=$(ls -r /opt/python | grep -E '^[34]' | tr '\n' ',' | sed 's/,$//')
    if [[ -z "$PYTHON_VERSIONS" ]]; then
        echo -e "   ${RED}No Posit Python versions installed${NC}"
    else
        echo -e "   ${GREEN}$PYTHON_VERSIONS${NC}"
    fi
else
    echo -e "   ${RED}No Posit Python versions installed${NC}"
fi && \

# List installed Quarto versions in /opt/quarto
echo -e ${BLUE}"Checking Installed Quarto Versions (/opt/quarto):"${NC} && \
if [[ -d "/opt/quarto" ]]; then
    QUARTO_VERSIONS=$(ls -r /opt/quarto 2>/dev/null | tr '\n' ',' | sed 's/,$//')
    if [[ -z "$QUARTO_VERSIONS" ]]; then
        echo -e "   ${RED}No Quarto versions installed${NC}"
    else
        echo -e "   ${GREEN}$QUARTO_VERSIONS${NC}"
    fi
else
    echo -e "   ${RED}No Quarto versions installed${NC}"
fi && \

# Check Internet Access
echo -e ${BLUE}"Checking Internet Access (google.com):"${NC} && \
RESPONSE_FILE=$(mktemp)
HTTP_CODE=$(curl -s -L -w "%{http_code}" -o "$RESPONSE_FILE" --max-time 5 "http://google.com" 2>/dev/null)
if [[ "$HTTP_CODE" =~ ^(2|3)[0-9]{2}$ ]] && grep -Eq "(Google Search|google\.com|gws_rd=)" "$RESPONSE_FILE"; then
    echo -e "   ${BLUE}Internet:${GREEN} Available${NC}"
else
    echo -e "   ${BLUE}Internet:${RED} Not Available${NC}"
fi
rm -f "$RESPONSE_FILE" && \

# Check Proxy Settings
echo -e ${BLUE}"Checking Proxy Settings:"${NC} && \
echo -e "   HTTP Proxy: ${NC}$(echo ${HTTP_PROXY:-None})" && \
echo -e "   HTTPS Proxy: ${NC}$(echo ${HTTPS_PROXY:-None})" && \

# Security Services Check
echo -e ${BLUE}"Checking Security Services:${NC}" && \
for sec_svc in iptables nftables firewalld; do
    if echo "$UNIT_FILES" | grep -q "^$sec_svc.service"; then
        status=$(systemctl is-active $sec_svc 2>/dev/null)
        if [[ "$status" == "active" ]]; then
            echo -e "   ${BLUE}$sec_svc:${GREEN} Installed & Active${NC}"
        else
            echo -e "   ${BLUE}$sec_svc:${YELLOW} Installed but Inactive${NC}"
        fi
    else
        echo -e "   ${BLUE}$sec_svc:${RED} Not Installed${NC}"
    fi
done && \

# Check SELinux Status
if command -v sestatus &> /dev/null; then
    SELINUX_STATUS=$(sestatus | awk '/SELinux status:/ {print $3}')
    SELINUX_CURRENT_MODE=$(sestatus | awk '/Current mode:/ {print $3}')
    SELINUX_CONFIG_MODE=$(sestatus | awk '/Mode from config file:/ {print $5}')
    if [[ "$SELINUX_STATUS" == "enabled" ]]; then
        echo -e "   ${BLUE}SELinux:${GREEN} Enabled (current: $SELINUX_CURRENT_MODE, reboot: $SELINUX_CONFIG_MODE)${NC}"
    else
        echo -e "   ${BLUE}SELinux:${RED} Disabled${NC}"
    fi
else
    echo -e "   ${BLUE}SELinux:${RED} Not Installed${NC}"
fi && \

# Check AppArmor Status
if command -v aa-status &> /dev/null; then
    APPARMOR_STATUS=$(aa-status --enforce | grep -c "enforce mode")
    if [[ "$APPARMOR_STATUS" -gt 0 ]]; then
        echo -e "   ${BLUE}AppArmor:${GREEN} Enabled${NC}"
    else
        echo -e "   ${BLUE}AppArmor:${RED} Disabled${NC}"
    fi
else
    echo -e "   ${BLUE}AppArmor:${RED} Not Installed${NC}"
fi