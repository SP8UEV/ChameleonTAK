import { Link } from 'react-router-dom';
import { Compass } from 'lucide-react';
import { Empty } from '../components/ui';

export default function NotFound() {
  return (
    <Empty
      icon={Compass}
      title="Nie ma takiej strony"
      action={
        <Link to="/" className="btn btn-primary">
          Wróć na pulpit
        </Link>
      }
    >
      Adres mógł się zmienić albo zawiera literówkę.
    </Empty>
  );
}
