export default function Badge({ state, label, small }) {
  return <span className={`badge ${state}${small ? ' small' : ''}`}>{label}</span>
}