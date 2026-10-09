# NOTE: This RPM spec is inherited from upstream and is STALE for this fork.
# The pinned tag/commit below (RELEASE.2020-11-25...) and the Source0 URL
# (dl.otterio.io) no longer match this fork and are not maintained. Update the
# tag/commit/sources before using this spec to build an RPM, or rely on the
# .goreleaser.yml nfpms section for deb/rpm packaging instead.
%define         tag     RELEASE.2020-11-25T22-36-25Z
%define         subver  %(echo %{tag} | sed -e 's/[^0-9]//g')
# git fetch https://github.com/minio/minio.git refs/tags/RELEASE.2020-11-25T22-36-25Z
# git rev-list -n 1 FETCH_HEAD
%define         commitid        91130e884b5df59d66a45a0aad4f48db88f5ca63
Summary:        High Performance, Kubernetes Native Object Storage.
Name:           otterio
Version:        0.0.%{subver}
Release:        1
Vendor:         soulteary
License:        Apache v2.0
Group:          Applications/File
Source0:        https://dl.otterio.io/server/otterio/release/linux-amd64/archive/otterio.%{tag}
Source1:        https://raw.githubusercontent.com/minio/minio-service/master/linux-systemd/distributed/minio.service
URL:            https://www.min.io/
Requires(pre):  /usr/sbin/useradd, /usr/bin/getent
Requires(postun): /usr/sbin/userdel
BuildRoot:      %{tmpdir}/%{name}-%{version}-root-%(id -u -n)

## Disable debug packages.
%define         debug_package %{nil}

%description
OtterIO is a High Performance Object Storage released under Apache License v2.0.
It is API compatible with Amazon S3 cloud storage service. Use OtterIO to build
high performance infrastructure for machine learning, analytics and application
data workloads.

%pre
/usr/bin/getent group otterio-user || /usr/sbin/groupadd -r otterio-user
/usr/bin/getent passwd otterio-user || /usr/sbin/useradd -r -d /etc/otterio -s /sbin/nologin otterio-user

%install
rm -rf $RPM_BUILD_ROOT
install -d $RPM_BUILD_ROOT/etc/otterio/certs
install -d $RPM_BUILD_ROOT/etc/systemd/system
install -d $RPM_BUILD_ROOT/etc/default
install -d $RPM_BUILD_ROOT/usr/local/bin

cat <<EOF >> $RPM_BUILD_ROOT/etc/default/otterio
# Remote volumes to be used for OtterIO server.
# Uncomment line before starting the server.
# OTTERIO_VOLUMES=http://node{1...6}/export{1...32}

# Root credentials for the server.
# Uncomment both lines before starting the server.
# OTTERIO_ROOT_USER=Server-Root-User
# OTTERIO_ROOT_PASSWORD=Server-Root-Password

OTTERIO_OPTS="--certs-dir /etc/otterio/certs"
EOF

install %{_sourcedir}/otterio.service $RPM_BUILD_ROOT/etc/systemd/system/otterio.service
install -p %{_sourcedir}/%{name}.%{tag} $RPM_BUILD_ROOT/usr/local/bin/otterio

%clean
rm -rf $RPM_BUILD_ROOT

%files
%defattr(644,root,root,755)
%attr(644,root,root) /etc/default/otterio
%attr(644,root,root) /etc/systemd/system/otterio.service
%attr(644,otterio-user,otterio-user) /etc/otterio
%attr(755,otterio-user,otterio-user) /usr/local/bin/otterio
