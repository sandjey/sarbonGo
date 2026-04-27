--
-- PostgreSQL database dump
--

\restrict DFO9xG1gpDGrTCkFL5qiNPL7Jh6Tjxkxt4P2OVVvv93Qmn1hpHsnhPwymiHVbGa

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: goadmin_user_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_user_permissions (
    user_id integer NOT NULL,
    permission_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_user_permissions goadmin_user_permissions_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_user_permissions
    ADD CONSTRAINT goadmin_user_permissions_unique UNIQUE (user_id, permission_id);


--
-- PostgreSQL database dump complete
--

\unrestrict DFO9xG1gpDGrTCkFL5qiNPL7Jh6Tjxkxt4P2OVVvv93Qmn1hpHsnhPwymiHVbGa

