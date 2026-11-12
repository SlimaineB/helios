export default function TunnelDiagram({ port, gateway }) {
  return (
    <div
      className="tunnel-diagram"
      role="img"
      aria-label={`Agentless architecture: the Helios hub exchanges status with a runner on the bastion, which runs Ansible over an SSH connection to 127.0.0.1:${port || '<port>'} and through the reverse tunnel opened by the managed host.`}
    >
      <div className="td-node">
        <span className="td-kicker">Control plane</span>
        <strong>Helios hub</strong>
        <span className="td-detail">Schedules apps, stores drift and results.</span>
      </div>
      <div className="td-link">
        <span className="td-arrow" aria-hidden="true">⇅</span>
        <span>HTTPS poll / results</span>
      </div>
      <div className="td-node td-bastion">
        <span className="td-kicker">Bastion</span>
        <strong>helios-runner</strong>
        <span className="td-detail">{gateway || 'Runner token and SSH identity'}</span>
      </div>
      <div className="td-link">
        <span className="td-arrow" aria-hidden="true">→</span>
        <span>ssh 127.0.0.1:{port || '<port>'}</span>
        <span className="td-detail">through the reverse tunnel</span>
      </div>
      <div className="td-node td-host">
        <span className="td-kicker">Managed host</span>
        <strong>sshd :22</strong>
        <span className="td-detail">Python 3, no Helios agent. Opens the tunnel outbound.</span>
      </div>
    </div>
  )
}
