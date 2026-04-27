--
-- PostgreSQL database dump
--

\restrict zdea0moofleOZM7LKfqcF1Vtvj5pID0a1Jesz7ToEHdFK5rszMBu35s8D8emKiE

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
-- Name: admins; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.admins (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    login character varying NOT NULL,
    password character varying NOT NULL,
    name character varying NOT NULL,
    status character varying DEFAULT 'active'::character varying NOT NULL,
    type character varying DEFAULT 'creator'::character varying NOT NULL,
    CONSTRAINT admins_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'inactive'::character varying, 'blocked'::character varying])::text[])))
);


--
-- Name: admins admins_login_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admins
    ADD CONSTRAINT admins_login_key UNIQUE (login);


--
-- Name: admins admins_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admins
    ADD CONSTRAINT admins_pkey PRIMARY KEY (id);


--
-- Name: idx_admins_login; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admins_login ON public.admins USING btree (login);


--
-- Name: admins admins_before_save_password; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admins_before_save_password BEFORE INSERT OR UPDATE OF password ON public.admins FOR EACH ROW EXECUTE FUNCTION public.admins_hash_password_trigger();


--
-- PostgreSQL database dump complete
--

\unrestrict zdea0moofleOZM7LKfqcF1Vtvj5pID0a1Jesz7ToEHdFK5rszMBu35s8D8emKiE

